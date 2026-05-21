import { useEffect, useRef, useState } from 'react';
import { GofugueClient, type ConnectionState } from '@gofugue/ipc';
import { useMudStore } from '@renderer/store/mud';
import { useProfileStore } from '@renderer/store/profiles';
import { useLayoutStore } from '@renderer/store/glLayout';

/** Singleton client shared for the app lifetime. */
let _client: GofugueClient | null = null;

function getClient(): GofugueClient {
  if (!_client) {
    _client = new GofugueClient({ url: 'ws://127.0.0.1:7879/' });
  }
  return _client;
}

/**
 * Connects to gofugue's WebSocket IPC, wires events to the Zustand store,
 * and exposes the client for sending commands.
 *
 * Auto-login: when a CONNECT hook fires for a world that has a pending
 * auto-login character set (via WorldManagerDialog "Connect as…"), the
 * character's connect string is expanded and sent line-by-line with a small
 * delay between commands, mimicking BeipMU's character auto-login behaviour.
 */
export function useGofugue() {
  const client = useRef(getClient());
  const [connState, setConnState] = useState<ConnectionState>(client.current.state);
  const { appendLine, updateStatus, ensureWorld } = useMudStore();

  useEffect(() => {
    const c = client.current;

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
          useLayoutStore.getState().ensureTerminalForWorld(ev.WorldName);
          await handleAutoLogin(c, ev.WorldName);
        } else if (ev.Name === 'QUIT') {
          window.gofugue?.quit?.();
        }
      }),
    ];

    c.connect();

    // Subscribe immediately (handles "already connected" on effect re-runs)
    // and re-subscribe after every future reconnect.
    // After subscribing, fetch current world status so we reflect any worlds
    // that gofugue already had connected before the frontend (re)connected.
    const doSubscribe = () => {
      c.subscribe(['world.line.rendered', 'hook', 'status', 'gmcp'])
        .then(() => c.worldsStatus())
        .then((worlds) => {
          const { ensureWorld, updateStatus } = useMudStore.getState();
          for (const w of worlds) {
            ensureWorld(w.name);
            updateStatus({ WorldName: w.name, Connected: w.connected, LagMS: 0 });
          }
          // Backfill scrollback for any world gofugue was already attached to
          // before this frontend connected. Bus events aren't replayed on new
          // subscribers, so without this the pane is blank until the next
          // MUD-sent line. history.tail returns world-tagged lines with
          // ANSI attrs intact, so colours are preserved.
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
        .catch(() => {
          // Not open yet — onStateChange will retry on next connect.
        });
    };

    doSubscribe();

    const subscribeOnConnect = c.onStateChange((s) => {
      if (s === 'connected') doSubscribe();
    });

    return () => {
      unsubs.forEach((u) => u());
      subscribeOnConnect();
      // Don't destroy — singleton lives for app lifetime.
    };
  }, [appendLine, updateStatus, ensureWorld]);

  return { client: client.current, connState };
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
