import type {
  EventType,
  WorldLineEvent,
  GMCPEvent,
  HookEvent,
  StatusEvent,
  AnyEvent,
  LineAttrs,
  JsonRpcNotification,
  JsonRpcResponse,
} from './types.js';

type EventMap = {
  'world.line': WorldLineEvent;
  'world.line.rendered': WorldLineEvent;
  gmcp: GMCPEvent;
  hook: HookEvent;
  status: StatusEvent;
  'user.input': AnyEvent;
  'user.cmd': AnyEvent;
};

type Handler<K extends EventType> = (params: EventMap[K]) => void;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyHandler = (params: any) => void;

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

export interface GofugueClientOptions {
  url?: string;
  /** Maximum reconnect backoff in ms. Default: 30_000 */
  maxReconnectDelay?: number;
}

/**
 * Typed WebSocket client for the gofugue JSON-RPC 2.0 IPC server.
 *
 * Usage:
 *   const client = new GofugueClient();
 *   client.on('world.line.rendered', (ev) => console.log(ev.Text));
 *   client.connect();
 *   await client.subscribe(['world.line.rendered', 'hook', 'status']);
 */
export class GofugueClient {
  private ws: WebSocket | null = null;
  private nextId = 1;
  private pending = new Map<
    number,
    { resolve: (v: unknown) => void; reject: (e: Error) => void }
  >();
  private handlers = new Map<string, Set<AnyHandler>>();
  private stateHandlers = new Set<(s: ConnectionState) => void>();

  private readonly url: string;
  private readonly maxDelay: number;
  private reconnectDelay = 1_000;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private _state: ConnectionState = 'disconnected';
  private destroyed = false;
  /** Events already subscribed on the current WebSocket connection. Cleared on close. */
  private subscribedEvents = new Set<string>();

  constructor(options: GofugueClientOptions = {}) {
    this.url = options.url ?? 'ws://127.0.0.1:7879/';
    this.maxDelay = options.maxReconnectDelay ?? 30_000;
  }

  get state(): ConnectionState {
    return this._state;
  }

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  connect(): void {
    // Idempotent: don't create a second WebSocket if already open or connecting.
    if (
      this.ws?.readyState === WebSocket.OPEN ||
      this.ws?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }
    this.destroyed = false;
    this._setState('connecting');
    this._open();
  }

  destroy(): void {
    this.destroyed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.ws?.close();
    this.ws = null;
    this._setState('disconnected');
  }

  // ── Event subscriptions ───────────────────────────────────────────────────

  on<K extends EventType>(event: K, handler: Handler<K>): () => void {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set());
    this.handlers.get(event)!.add(handler as AnyHandler);
    return () => this.handlers.get(event)?.delete(handler as AnyHandler);
  }

  onStateChange(handler: (s: ConnectionState) => void): () => void {
    this.stateHandlers.add(handler);
    return () => this.stateHandlers.delete(handler);
  }

  // ── RPC methods ───────────────────────────────────────────────────────────

  call<T = unknown>(method: string, params?: unknown): Promise<T> {
    return new Promise((resolve, reject) => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
        reject(new Error('Not connected'));
        return;
      }
      const id = this.nextId++;
      this.pending.set(id, {
        resolve: resolve as (v: unknown) => void,
        reject,
      });
      const msg = JSON.stringify({ jsonrpc: '2.0', id, method, params });
      this.ws.send(msg);
    });
  }

  subscribe(events: EventType[]): Promise<{ ok: boolean }> {
    // Idempotent: only send events not yet subscribed on this connection.
    // gofugue subscriptions are additive and permanent — calling subscribe
    // twice for the same event causes the server to emit it twice.
    const fresh = events.filter((e) => !this.subscribedEvents.has(e));
    if (fresh.length === 0) return Promise.resolve({ ok: true });
    fresh.forEach((e) => this.subscribedEvents.add(e));
    return this.call('subscribe', { events: fresh });
  }

  subscribeAll(): Promise<{ ok: boolean }> {
    return this.call('subscribe', { events: [] });
  }

  input(text: string, world?: string): Promise<{ ok: boolean }> {
    return this.call('input', { text, ...(world ? { world } : {}) });
  }

  cmd(line: string, world?: string): Promise<{ ok: boolean }> {
    return this.call('cmd', { line, ...(world ? { world } : {}) });
  }

  history(n = 200): Promise<string[]> {
    return this.call('history.get', { n });
  }

  /** World-tagged scrollback with ANSI attributes preserved. */
  historyTail(n = 200): Promise<{ world: string; text: string; attrs: LineAttrs }[]> {
    return this.call('history.tail', { n });
  }

  worldsStatus(): Promise<{ name: string; connected: boolean }[]> {
    return this.call('worlds.status');
  }

  // ── Internal ──────────────────────────────────────────────────────────────

  private _setState(s: ConnectionState): void {
    if (this._state === s) return;
    this._state = s;
    this.stateHandlers.forEach((h) => h(s));
  }

  private _open(): void {
    if (this.destroyed) return;

    const ws = new WebSocket(this.url);
    this.ws = ws;

    ws.onopen = () => {
      this.reconnectDelay = 1_000;
      this._setState('connected');
    };

    ws.onmessage = (e: MessageEvent<string>) => {
      let msg: JsonRpcNotification | JsonRpcResponse;
      try {
        msg = JSON.parse(e.data) as JsonRpcNotification | JsonRpcResponse;
      } catch {
        return;
      }

      if ('id' in msg && ('result' in msg || 'error' in msg)) {
        // Response to one of our calls.
        const r = msg as JsonRpcResponse;
        const p = this.pending.get(r.id);
        if (p) {
          this.pending.delete(r.id);
          if (r.error) p.reject(new Error(r.error.message));
          else p.resolve(r.result);
        }
      } else {
        // Server-push notification.
        const n = msg as JsonRpcNotification;
        this.handlers.get(n.method)?.forEach((h) => h(n.params));
      }
    };

    ws.onclose = () => {
      // Subscriptions are per-connection; clear so reconnect re-subscribes.
      this.subscribedEvents.clear();
      if (this.destroyed) return;
      this._setState('reconnecting');
      this.reconnectTimer = setTimeout(() => {
        this._open();
      }, this.reconnectDelay);
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxDelay);
    };

    ws.onerror = () => ws.close();
  }
}
