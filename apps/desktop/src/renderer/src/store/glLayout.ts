/**
 * Minimal store that:
 *  - Persists the GoldenLayout config to localStorage.
 *  - Holds a reference to the live GoldenLayout instance so other parts of the
 *    app can add panels imperatively (e.g. on CONNECT hook).
 */
import { create } from 'zustand';
import {
  type GoldenLayout,
  type ResolvedLayoutConfig,
  type ContentItem,
  type ComponentItem,
  ContentItem as ContentItemClass,
  type Stack,
} from 'golden-layout';
import type { PanelState } from '@renderer/features/layout/GoldenLayoutRoot';

const urlParams = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '');
const windowId = urlParams.get('windowId') || 'default';
const STORAGE_KEY = `gofugue-gl-layout-v2-${windowId}`;

/** Recursively walk the GL content tree to find a terminal panel for a world. */
function findTerminal(item: ContentItem | undefined, worldName: string): ComponentItem | undefined {
  if (!item) return undefined;
  if (ContentItemClass.isComponentItem(item)) {
    const cfg = item.toConfig();
    const state = cfg.componentState as PanelState | undefined;
    if (cfg.componentType === 'terminal' && state?.kind === 'terminal' && state.worldName === worldName) {
      return item;
    }
    return undefined;
  }
  for (const child of item.contentItems) {
    const found = findTerminal(child, worldName);
    if (found) return found;
  }
  return undefined;
}

interface GlLayoutStore {
  gl: GoldenLayout | null;
  setGl: (gl: GoldenLayout) => void;

  saveConfig: (config: ResolvedLayoutConfig) => void;
  loadConfig: () => ResolvedLayoutConfig | null;

  /** Add a panel. If GL has a focused stack it goes there; otherwise the root. */
  addPanel: (state: PanelState, title: string) => void;

  /** Ensure a terminal panel exists for worldName; no-op if already open. */
  ensureTerminalForWorld: (worldName: string) => void;
}

export const useLayoutStore = create<GlLayoutStore>()((set, get) => ({
  gl: null,

  setGl(gl) {
    set({ gl });
  },

  saveConfig(config) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
    } catch {}
  },

  loadConfig() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      return raw ? (JSON.parse(raw) as ResolvedLayoutConfig) : null;
    } catch {
      return null;
    }
  },

  addPanel(state, title) {
    const { gl } = get();
    if (!gl) return;

    const componentType = state.kind;

    // Try to add to the focused (selected) stack
    try {
      const focused = gl.focusedComponentItem;
      if (focused && focused.parentItem && ContentItemClass.isStack(focused.parentItem)) {
        (focused.parentItem as Stack).addComponent(componentType, state, title);
        return;
      }
    } catch {}

    // No focused item — add using GL's default location selectors
    try {
      gl.addComponent(componentType, state, title);
    } catch {
      gl.newComponent(componentType, state, title);
    }
  },

  ensureTerminalForWorld(worldName) {
    const { gl, addPanel } = get();
    if (!gl) return;

    // Walk the content tree looking for an existing terminal for this world
    const existing = findTerminal(gl.rootItem, worldName);
    if (existing) {
      existing.focus();
      return;
    }

    // Not found — add a new terminal panel
    addPanel({ kind: 'terminal', worldName }, worldName);
  },
}));
