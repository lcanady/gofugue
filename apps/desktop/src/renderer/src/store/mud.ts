import { create } from 'zustand';
import type { WorldLineEvent, StatusEvent } from '@gofugue/ipc';

/** Maximum lines retained per world in the scrollback buffer. */
const MAX_SCROLLBACK = 50_000;

export interface WorldState {
  name: string;
  connected: boolean;
  /** True once the first status event has been received for this world. */
  statusReceived: boolean;
  lagMS: number;
  /** Ordered scrollback lines (oldest first). */
  lines: WorldLineEvent[];
  /** True when the user has scrolled up away from the live tail. */
  scrollLocked: boolean;
}

interface MudStore {
  /** Active world names in tab order. */
  worldOrder: string[];
  worlds: Record<string, WorldState>;
  activeWorld: string | null;

  // ── Mutations ─────────────────────────────────────────────────────────

  appendLine: (ev: WorldLineEvent) => void;
  updateStatus: (ev: StatusEvent) => void;
  ensureWorld: (name: string) => void;
  setActiveWorld: (name: string) => void;
  setScrollLocked: (world: string, locked: boolean) => void;
  clearWorld: (name: string) => void;
  removeWorld: (name: string) => void;
}

function makeWorld(name: string): WorldState {
  return {
    name,
    connected: false,
    statusReceived: false,
    lagMS: 0,
    lines: [],
    scrollLocked: false,
  };
}

export const useMudStore = create<MudStore>((set) => ({
  worldOrder: [],
  worlds: {},
  activeWorld: null,

  ensureWorld(name) {
    // "local" is gofugue's internal channel for system messages — never a tab.
    if (name === 'local') return;
    set((s) => {
      if (s.worlds[name]) return s;
      return {
        worlds: { ...s.worlds, [name]: makeWorld(name) },
        worldOrder: s.worldOrder.includes(name) ? s.worldOrder : [...s.worldOrder, name],
        activeWorld: s.activeWorld ?? name,
      };
    });
  },

  appendLine(ev) {
    if (ev.Gagged) return;
    set((s) => {
      // "local" = gofugue system messages. Store lines but never add to
      // worldOrder or promote to activeWorld.
      const isLocal = ev.WorldName === 'local';

      const addLine = (existing: WorldState | undefined, name: string): WorldState => {
        const w = existing ?? makeWorld(name);
        const lines =
          w.lines.length >= MAX_SCROLLBACK
            ? [...w.lines.slice(-MAX_SCROLLBACK + 1), ev]
            : [...w.lines, ev];
        return { ...w, lines };
      };

      let nextWorlds = {
        ...s.worlds,
        [ev.WorldName]: addLine(s.worlds[ev.WorldName], ev.WorldName),
      };

      // Mirror local messages into the active world's scrollback so the user
      // sees errors/system output in context without a dedicated "local" tab.
      if (isLocal && s.activeWorld && s.activeWorld !== 'local') {
        nextWorlds = {
          ...nextWorlds,
          [s.activeWorld]: addLine(nextWorlds[s.activeWorld], s.activeWorld),
        };
      }

      return {
        worlds: nextWorlds,
        worldOrder: isLocal || s.worldOrder.includes(ev.WorldName)
          ? s.worldOrder
          : [...s.worldOrder, ev.WorldName],
        activeWorld: isLocal ? s.activeWorld : (s.activeWorld ?? ev.WorldName),
      };
    });
  },

  updateStatus(ev) {
    set((s) => {
      // C4: never create a world from a status event — only update existing ones.
      // worldsStatus hydration (on subscribe) must not auto-create tabs for
      // worlds the user never opened.
      const existing = s.worlds[ev.WorldName];
      if (!existing) return s;
      return {
        worlds: {
          ...s.worlds,
          [ev.WorldName]: {
            ...existing,
            connected: ev.Connected,
            lagMS: ev.LagMS,
            statusReceived: true,
          },
        },
      };
    });
  },

  setActiveWorld(name) {
    set({ activeWorld: name });
  },

  setScrollLocked(world, locked) {
    set((s) => ({
      worlds: {
        ...s.worlds,
        [world]: { ...(s.worlds[world] ?? makeWorld(world)), scrollLocked: locked },
      },
    }));
  },

  clearWorld(name) {
    set((s) => ({
      worlds: {
        ...s.worlds,
        [name]: { ...(s.worlds[name] ?? makeWorld(name)), lines: [] },
      },
    }));
  },

  removeWorld(name) {
    set((s) => {
      const { [name]: _removed, ...rest } = s.worlds;
      const worldOrder = s.worldOrder.filter((w) => w !== name);
      const activeWorld =
        s.activeWorld === name ? (worldOrder[0] ?? null) : s.activeWorld;
      return { worlds: rest, worldOrder, activeWorld };
    });
  },
}));
