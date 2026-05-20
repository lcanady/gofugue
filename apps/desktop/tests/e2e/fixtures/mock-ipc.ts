/**
 * Playwright fixture that intercepts the gofugue WebSocket IPC
 * (ws://127.0.0.1:7879/) and provides a test-controlled mock server.
 *
 * The mock:
 *   - Auto-responds to JSON-RPC method calls with {result:{ok:true}}
 *   - Exposes `pushEvent` to push server-sent notifications to the page
 *   - Exposes `waitConnected` to synchronise with the React mount cycle
 *   - Exposes `messages` to assert what the page sent
 */
import { test as base, expect } from '@playwright/test';

export interface MockIPC {
  /** Resolves when the page has opened the WebSocket connection. */
  waitConnected(timeoutMs?: number): Promise<void>;
  /**
   * Push a JSON-RPC server notification to the page.
   * `method` is the event type (e.g. "world.line.rendered").
   */
  pushEvent(method: string, params: unknown): void;
  /** All JSON-RPC objects received from the page so far. */
  readonly messages: unknown[];
}

export const test = base.extend<{ mockIPC: MockIPC }>({
  mockIPC: async ({ page }, use) => {
    const received: unknown[] = [];
    let sendFn: ((data: string) => void) | null = null;
    let connectedResolve!: () => void;
    const connectedPromise = new Promise<void>((res) => {
      connectedResolve = res;
    });

    // Set up the intercept BEFORE the page navigates so the first connect
    // attempt is caught. GofugueClient auto-reconnects, so later retries are
    // caught too.
    await page.routeWebSocket('ws://127.0.0.1:7879/', (ws) => {
      sendFn = (data) => ws.send(data);
      connectedResolve();

      ws.onMessage((msg) => {
        const str = typeof msg === 'string' ? msg : Buffer.from(msg as Buffer).toString();
        let parsed: unknown;
        try {
          parsed = JSON.parse(str);
        } catch {
          return;
        }
        received.push(parsed);

        // Auto-reply to any JSON-RPC call (has an "id" field).
        const req = parsed as Record<string, unknown>;
        if (req['id'] != null) {
          ws.send(
            JSON.stringify({ jsonrpc: '2.0', id: req['id'], result: { ok: true } }),
          );
        }
      });
    });

    const mock: MockIPC = {
      waitConnected(timeoutMs = 5_000) {
        return Promise.race([
          connectedPromise,
          new Promise<void>((_, reject) =>
            setTimeout(
              () => reject(new Error('MockIPC: timed out waiting for WebSocket connect')),
              timeoutMs,
            ),
          ),
        ]);
      },

      pushEvent(method, params) {
        if (!sendFn) {
          throw new Error('MockIPC: WebSocket not connected yet — call waitConnected() first');
        }
        sendFn(JSON.stringify({ jsonrpc: '2.0', method, params }));
      },

      get messages() {
        return [...received];
      },
    };

    await use(mock);
  },
});

export { expect };
