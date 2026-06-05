import { useEffect, useRef, useState } from 'react';
import { GofugueClient, type ConnectionState } from '@gofugue/ipc';
import { useMudStore } from '@renderer/store/mud';
import { useProfileStore } from '@renderer/store/profiles';
import { useLayoutStore } from '@renderer/store/glLayout';

/** Singleton client shared for the app lifetime. */
let _client: GofugueClient | null = null;

/** Fetch the IPC auth token from the main process (Electron only). */
async function fetchIPCToken(): Promise<string> {
  try {
    // window.gofugue is exposed by the preload script via contextBridge.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const api = (window as any).gofugue;
    if (api?.getIPCToken) {
      return (await api.getIPCToken()) as string;
    }
  } catch {
    // Not running under Electron or preload not yet ready — no token.
  }
  return '';
}

// Kick off the token fetch eagerly at module load time so the client is ready
// by the time the first useGofugue() hook runs.
const _tokenPromise: Promise<string> = fetchIPCToken();

/**
 * Lazily create (or return) the singleton GofugueClient.
 * On first call the token must already be resolved (call ensureClientReady
 * first); subsequent calls always return the same instance.
 */
function getClient(token?: string): GofugueClient {
  if (!_client) {
    _client = new GofugueClient({ url: 'ws://127.0.0.1:7879/', token });
  }
  return _client;
}

/**
 * Ensures the singleton client is created with the resolved IPC token before
 * connect() is called. Safe to await multiple times — returns immediately on
 * subsequent calls.
 */
async function ensureClientReady(): Promise<GofugueClient> {
  if (_client) return _client;
  const tok = await _tokenPromise;
  return getClient(tok || undefined);
}

const activeIntervals = new Map<string, number[]>();
const registeredMacros = new Map<string, Set<string>>();

function parseDurationToMS(dur: string): number {
  const match = dur.trim().match(/^(\d+)(ms|s|m|h)?$/);
  if (!match) return 10000;
  const val = parseInt(match[1]);
  const unit = match[2] || 's';
  switch (unit) {
    case 'ms': return val;
    case 's': return val * 1000;
    case 'm': return val * 60 * 1000;
    case 'h': return val * 60 * 60 * 1000;
    default: return val * 1000;
  }
}

function quoteArg(val: string): string {
  if (!val) return '""';
  if (val.includes('"')) {
    return `'${val}'`;
  }
  return `"${val}"`;
}

export async function syncWorldMacros(client: GofugueClient, worldProfile: any, worldName: string) {
  // First clean up previous macros
  await undefineWorldMacros(client, worldName);

  const macroNames = new Set<string>();

  // Register Triggers
  for (const trg of worldProfile.triggers ?? []) {
    if (!trg.enabled || !trg.pattern.trim()) continue;
    const pat = quoteArg(trg.pattern);
    const mname = `trg_${trg.id}`;
    macroNames.add(mname);

    if (trg.type === 'trigger') {
      await client.cmd(`/def -t${pat} -m${trg.matchMode} -p${trg.priority} -w"${worldName}" ${mname}=${trg.body}`, worldName);
    } else if (trg.type === 'gag') {
      await client.cmd(`/gag -t${pat} -m${trg.matchMode} -p${trg.priority} -w"${worldName}" ${mname}=`, worldName);
    } else if (trg.type === 'hilite') {
      await client.cmd(`/hilite -t${pat} -m${trg.matchMode} -p${trg.priority} -w"${worldName}" ${mname}=${trg.body}`, worldName);
    } else if (trg.type === 'substitute') {
      await client.cmd(`/substitute -t${pat} -m${trg.matchMode} -p${trg.priority} -w"${worldName}" ${mname}=${trg.body}`, worldName);
    }
  }

  // Register Aliases
  for (const alias of worldProfile.aliases ?? []) {
    if (!alias.enabled || !alias.pattern.trim()) continue;
    const mname = `alias_${worldName}_${alias.pattern}`;
    macroNames.add(mname);
    await client.cmd(`/alias -w"${worldName}" ${alias.pattern}=${alias.body}`, worldName);
  }

  // Sidecar Scripts
  if (worldProfile.jsScriptEnabled && worldProfile.jsScriptPath.trim()) {
    await client.cmd(`/js ${worldProfile.jsScriptPath.trim()}`);
  }
  if (worldProfile.pyScriptEnabled && worldProfile.pyScriptPath.trim()) {
    await client.cmd(`/py ${worldProfile.pyScriptPath.trim()}`);
  }

  // TinyFugue Script
  if (worldProfile.tfScriptEnabled && worldProfile.tfScriptPath.trim()) {
    await client.cmd(`/load ${worldProfile.tfScriptPath.trim()}`);
  }

  // Global Variables — /set name=value
  for (const v of worldProfile.variables ?? []) {
    if (!v.enabled || !v.name.trim()) continue;
    await client.cmd(`/set ${v.name.trim()}=${v.value}`);
  }

  // Safety Restrictions — /restrict CAPABILITY
  if (worldProfile.restrictShell) {
    await client.cmd('/restrict SHELL');
  }
  if (worldProfile.restrictFile) {
    await client.cmd('/restrict FILE');
  }
  if (worldProfile.restrictWorld) {
    await client.cmd('/restrict WORLD');
  }

  registeredMacros.set(worldName, macroNames);
}

export async function undefineWorldMacros(client: GofugueClient, worldName: string) {
  const macroNames = registeredMacros.get(worldName);
  if (macroNames) {
    for (const name of macroNames) {
      try {
        await client.cmd(`/undef ${name}`, worldName);
      } catch {
        // ignore errors
      }
    }
    registeredMacros.delete(worldName);
  }
}

export function rescheduleTimers(client: GofugueClient, worldName: string, worldProfile: any) {
  clearWorldTimers(worldName);

  const intervals: number[] = [];

  for (const timer of worldProfile.timers ?? []) {
    if (!timer.enabled || !timer.body.trim()) continue;
    const ms = parseDurationToMS(timer.duration);

    const run = () => {
      const body = timer.body.trim();
      if (body.startsWith('/')) {
        client.cmd(body, worldName);
      } else {
        client.input(body, worldName);
      }
    };

    if (timer.repeat) {
      const id = window.setInterval(run, ms);
      intervals.push(id);
    } else {
      const id = window.setTimeout(run, ms);
      intervals.push(id);
    }
  }

  activeIntervals.set(worldName, intervals);
}

export function clearWorldTimers(worldName: string) {
  const ids = activeIntervals.get(worldName);
  if (ids) {
    for (const id of ids) {
      window.clearInterval(id);
      window.clearTimeout(id);
    }
    activeIntervals.delete(worldName);
  }
}

/**
 * Connects to gofugue's WebSocket IPC, wires events to the Zustand store,
 * and exposes the client for sending commands.
 */
export function useGofugue() {
  // Use the singleton client if already created; ensureClientReady() will
  // initialise it with the auth token before connect() is called.
  const client = useRef(getClient());
  const [connState, setConnState] = useState<ConnectionState>(client.current.state);
  const { appendLine, updateStatus, ensureWorld } = useMudStore();

  useEffect(() => {
    let cancelled = false;
    let cleanup: (() => void) | undefined;

    ensureClientReady().then((c) => {
      if (cancelled) return;
      // Update the ref so the rest of the hook and returned value see the
      // same client instance (in practice _client is already set, so this
      // is a no-op ref update).
      client.current = c;
      cleanup = _setupClientHandlers(c, appendLine, updateStatus, ensureWorld, setConnState);
    });

    return () => {
      cancelled = true;
      cleanup?.();
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return { client: client.current, connState };
}

/** Internal: wire all event handlers and connect. Returns a cleanup fn. */
function _setupClientHandlers(
  c: GofugueClient,
  appendLine: ReturnType<typeof useMudStore>['appendLine'],
  updateStatus: ReturnType<typeof useMudStore>['updateStatus'],
  ensureWorld: ReturnType<typeof useMudStore>['ensureWorld'],
  setConnState: (s: ConnectionState) => void,
): () => void {
    const onGlobalKey = (e: KeyboardEvent) => {
      const activeWorldName = useMudStore.getState().activeWorld;
      if (!activeWorldName) return;

      const store = useProfileStore.getState();
      const worldProfile = store.worlds.find(
        (w) => store.worldNameFromUrl(w.url) === activeWorldName
      );
      if (!worldProfile) return;

      const parts: string[] = [];
      if (e.ctrlKey) parts.push('ctrl');
      if (e.altKey) parts.push('alt');
      if (e.shiftKey) parts.push('shift');
      if (e.metaKey) parts.push('meta');

      let keyName = e.key.toLowerCase();
      if (keyName === 'arrowup') keyName = 'up';
      else if (keyName === 'arrowdown') keyName = 'down';
      else if (keyName === 'arrowleft') keyName = 'left';
      else if (keyName === 'arrowright') keyName = 'right';
      else if (keyName === ' ') keyName = 'space';

      if (!['control', 'alt', 'shift', 'meta'].includes(keyName)) {
        parts.push(keyName);
      }
      
      const combo = parts.join('+');

      const match = worldProfile.keybindings?.find(
        (kb) => kb.enabled && kb.key.toLowerCase() === combo
      );
      if (match) {
        const activeEl = document.activeElement;
        const isInputActive = activeEl && (activeEl.tagName === 'INPUT' || activeEl.tagName === 'TEXTAREA');

        if (isInputActive) {
          const isPlainChar = combo.length === 1 || (combo.length > 0 && !combo.includes('+') && !combo.startsWith('f') && combo !== 'escape');
          if (isPlainChar) return;
        }

        e.preventDefault();
        e.stopPropagation();

        if (match.body.trim().startsWith('/')) {
          c.cmd(match.body.trim(), activeWorldName);
        } else {
          c.input(match.body.trim(), activeWorldName);
        }
      }
    };

    window.addEventListener('keydown', onGlobalKey);

    const unsubs = [
      c.onStateChange(setConnState),

      c.on('world.line.rendered', (ev) => {
        ensureWorld(ev.WorldName);
        appendLine(ev);
      }),

      c.on('status', (ev) => {
        ensureWorld(ev.WorldName);
        updateStatus(ev);
      }),

      c.on('hook', async (ev) => {
        if (ev.Name === 'CONNECT') {
          ensureWorld(ev.WorldName);
          const isElectron = typeof window.electron !== 'undefined';
          const wasInitiatedHere = useMudStore.getState().connectingWorlds[ev.WorldName];
          const shouldHandle = !isElectron || document.hasFocus() || wasInitiatedHere;
          if (shouldHandle) {
            useMudStore.getState().setConnectingWorld(ev.WorldName, false);
            useLayoutStore.getState().ensureTerminalForWorld(ev.WorldName);
            
            // Sync triggers, aliases, and timers
            const profile = useProfileStore.getState().worlds.find(
              (wp) => useProfileStore.getState().worldNameFromUrl(wp.url) === ev.WorldName
            );
            if (profile) {
              await syncWorldMacros(c, profile, ev.WorldName);
              rescheduleTimers(c, ev.WorldName, profile);
            }

            await handleAutoLogin(c, ev.WorldName);
          }
        } else if (ev.Name === 'QUIT' || ev.Name === 'DISCONNECT') {
          clearWorldTimers(ev.WorldName);
          await undefineWorldMacros(c, ev.WorldName);
          useMudStore.getState().removeWorld(ev.WorldName);
        }
      }),
    ];

    c.connect();

    const doSubscribe = () => {
      c.subscribe(['world.line.rendered', 'hook', 'status', 'gmcp'])
        .then(() => c.worldsStatus())
        .then((worlds) => {
          const { ensureWorld, updateStatus } = useMudStore.getState();
          for (const w of worlds) {
            ensureWorld(w.name);
            updateStatus({ WorldName: w.name, Connected: w.connected, LagMS: 0 });
          }
          return c.historyTail(500);
        })
        .then((lines) => {
          if (!lines?.length) return;
          const { appendLine, ensureWorld } = useMudStore.getState();
          for (const l of lines) {
            ensureWorld(l.world);
            appendLine({
              WorldName: l.world,
              Text: l.text,
              Attrs: l.attrs,
              Spans: null,
              Gagged: false,
            });
          }
        })
        .catch(() => {});
    };

    doSubscribe();

    const subscribeOnConnect = c.onStateChange((s) => {
      if (s === 'connected') doSubscribe();
    });

    return () => {
      unsubs.forEach((u) => u());
      subscribeOnConnect();
      window.removeEventListener('keydown', onGlobalKey);
      for (const wname of activeIntervals.keys()) {
        clearWorldTimers(wname);
      }
    };
}

/**
 * If there is a pending auto-login for the given world name, decrypt the
 * character's password, expand the connect string, and send each line with
 * 300 ms between commands.
 */
async function handleAutoLogin(client: GofugueClient, worldName: string): Promise<void> {
  const store = useProfileStore.getState();
  const charId = store.pendingAutoLogin[worldName];
  if (!charId) return;

  // Find the character across all worlds.
  let char: import('@renderer/store/profiles').CharacterProfile | undefined;
  for (const w of store.worlds) {
    const found = w.characters.find((c) => c.id === charId);
    if (found) { char = found; break; }
  }

  if (!char || !char.connectString.trim()) {
    store.clearPendingAutoLogin(worldName);
    return;
  }

  store.clearPendingAutoLogin(worldName);

  // Decrypt password if stored.
  let password = '';
  if (char.encryptedPassword && window.gofugue?.decryptPassword) {
    try {
      password = await window.gofugue.decryptPassword(char.encryptedPassword);
    } catch {
      // Proceed without password.
    }
  }

  // Expand macros and send each non-empty line.
  const lines = char.connectString
    .split('\n')
    .map((l) => l.replace(/\{password\}/gi, password).replace(/\{name\}/gi, char!.name))
    .filter((l) => l.trim());

  for (let i = 0; i < lines.length; i++) {
    // Stagger commands so the server processes them in order.
    setTimeout(() => {
      client.input(lines[i], worldName);
    }, i * 300);
  }
}
