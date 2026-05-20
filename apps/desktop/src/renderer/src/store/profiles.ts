/**
 * World & Character profile store — persisted to localStorage.
 *
 * Hierarchy mirrors BeipMU:  World > Character
 *
 * Each World stores a connection URL and display preferences.
 * Each Character stores a connect string (multi-line commands sent after
 * CONNECT) and an encrypted password blob (via Electron safeStorage IPC).
 *
 * Passwords are stored as base64(safeStorage.encryptString(plain)) so they
 * are OS-keychain-backed without requiring a separate secrets file.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export interface CharacterProfile {
  id: string;
  name: string;
  /** Newline-separated commands sent after CONNECT; use {password} macro. */
  connectString: string;
  /** base64(safeStorage.encryptString(password)). Empty string = no password. */
  encryptedPassword: string;
  notes: string;
}

export interface WorldProfile {
  id: string;
  /** Display name shown in the World Manager list. */
  name: string;
  /** mud:// muds:// ws:// wss:// URL */
  url: string;
  /** 'utf-8' | 'latin1' */
  encoding: string;
  /** GMCP enable flag (sent to gofugue via connect string or config). */
  gmcp: boolean;
  notes: string;
  characters: CharacterProfile[];
}

interface ProfileStore {
  worlds: WorldProfile[];

  /**
   * Maps worldName (the gofugue world name, derived from URL host) to the
   * characterId that should auto-login on the next CONNECT hook.
   */
  pendingAutoLogin: Record<string, string>;

  // ── World CRUD ────────────────────────────────────────────────────────────

  addWorld(partial?: Partial<WorldProfile>): WorldProfile;
  updateWorld(id: string, patch: Partial<WorldProfile>): void;
  deleteWorld(id: string): void;

  // ── Character CRUD ────────────────────────────────────────────────────────

  addCharacter(worldId: string, partial?: Partial<CharacterProfile>): CharacterProfile;
  updateCharacter(worldId: string, charId: string, patch: Partial<CharacterProfile>): void;
  deleteCharacter(worldId: string, charId: string): void;

  // ── Auto-login coordination ───────────────────────────────────────────────

  setPendingAutoLogin(worldName: string, charId: string): void;
  clearPendingAutoLogin(worldName: string): void;

  /** Derive a short world name from a URL (the hostname part). */
  worldNameFromUrl(url: string): string;
}

function makeWorld(partial: Partial<WorldProfile> = {}): WorldProfile {
  return {
    id: crypto.randomUUID(),
    name: partial.name ?? 'New World',
    url: partial.url ?? 'mud://',
    encoding: partial.encoding ?? 'utf-8',
    gmcp: partial.gmcp ?? true,
    notes: partial.notes ?? '',
    characters: partial.characters ?? [],
  };
}

function makeCharacter(partial: Partial<CharacterProfile> = {}): CharacterProfile {
  return {
    id: crypto.randomUUID(),
    name: partial.name ?? 'New Character',
    connectString: partial.connectString ?? '',
    encryptedPassword: partial.encryptedPassword ?? '',
    notes: partial.notes ?? '',
  };
}

export const useProfileStore = create<ProfileStore>()(
  persist(
    (set, get) => ({
      worlds: [],
      pendingAutoLogin: {},

      addWorld(partial) {
        const world = makeWorld(partial);
        set((s) => ({ worlds: [...s.worlds, world] }));
        return world;
      },

      updateWorld(id, patch) {
        set((s) => ({
          worlds: s.worlds.map((w) => (w.id === id ? { ...w, ...patch } : w)),
        }));
      },

      deleteWorld(id) {
        set((s) => ({ worlds: s.worlds.filter((w) => w.id !== id) }));
      },

      addCharacter(worldId, partial) {
        const char = makeCharacter(partial);
        set((s) => ({
          worlds: s.worlds.map((w) =>
            w.id === worldId ? { ...w, characters: [...w.characters, char] } : w,
          ),
        }));
        return char;
      },

      updateCharacter(worldId, charId, patch) {
        set((s) => ({
          worlds: s.worlds.map((w) =>
            w.id === worldId
              ? {
                  ...w,
                  characters: w.characters.map((c) =>
                    c.id === charId ? { ...c, ...patch } : c,
                  ),
                }
              : w,
          ),
        }));
      },

      deleteCharacter(worldId, charId) {
        set((s) => ({
          worlds: s.worlds.map((w) =>
            w.id === worldId
              ? { ...w, characters: w.characters.filter((c) => c.id !== charId) }
              : w,
          ),
        }));
      },

      setPendingAutoLogin(worldName, charId) {
        set((s) => ({
          pendingAutoLogin: { ...s.pendingAutoLogin, [worldName]: charId },
        }));
      },

      clearPendingAutoLogin(worldName) {
        set((s) => {
          const next = { ...s.pendingAutoLogin };
          delete next[worldName];
          return { pendingAutoLogin: next };
        });
      },

      worldNameFromUrl(url) {
        try {
          const u = new URL(url.replace(/^mud/, 'tcp').replace(/^muds/, 'tcp'));
          return u.hostname || url;
        } catch {
          return url;
        }
      },
    }),
    {
      name: 'gofugue-profiles',
      version: 1,
      // pendingAutoLogin is session-only — never persist it across restarts.
      partialize: (s) => ({ worlds: s.worlds }),
      migrate: (persisted: unknown) => {
        // v0 → v1: strip stale pendingAutoLogin from storage.
        const p = persisted as Record<string, unknown> | null;
        return { worlds: (p?.worlds ?? []) as WorldProfile[] };
      },
    },
  ),
);
