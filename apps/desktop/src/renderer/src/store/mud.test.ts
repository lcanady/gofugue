/**
 * TDD Audit — mud store unit tests.
 *
 * Covers:
 *  C4  worldsStatus must not create new world tabs (only update existing)
 *  H1  "local" world must never appear as a tab or become activeWorld
 *  H3  removeWorld must leave other worlds intact and update activeWorld correctly
 *  C1  ensureWorld: activeWorld tracks first world, not overwritten by second
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { useMudStore } from './mud';
import type { WorldLineEvent, StatusEvent } from '@gofugue/ipc';

function makeLineEvent(worldName: string, text = 'hello'): WorldLineEvent {
  return {
    WorldName: worldName,
    Text: text,
    Attrs: { FG: -1, BG: -1, Bold: false, Underline: false, Italic: false, Reverse: false, FGRGB: [0,0,0], BGRGB: [0,0,0] },
    Spans: null,
    Gagged: false,
  };
}

function makeStatusEvent(worldName: string, connected: boolean): StatusEvent {
  return { WorldName: worldName, Connected: connected, LagMS: 0 };
}

beforeEach(() => {
  // Reset store to initial state before each test
  useMudStore.setState({
    worldOrder: [],
    worlds: {},
    activeWorld: null,
  });
});

// ── H1: "local" world isolation ──────────────────────────────────────────────

describe('H1 — "local" system world never appears as a tab', () => {
  it('appendLine for "local" does not add "local" to worldOrder', () => {
    useMudStore.getState().appendLine(makeLineEvent('local', 'Error: something'));
    expect(useMudStore.getState().worldOrder).not.toContain('local');
  });

  it('appendLine for "local" does not set activeWorld to "local"', () => {
    useMudStore.getState().appendLine(makeLineEvent('local'));
    expect(useMudStore.getState().activeWorld).toBeNull();
  });

  it('ensureWorld("local") is a no-op', () => {
    useMudStore.getState().ensureWorld('local');
    expect(useMudStore.getState().worldOrder).not.toContain('local');
    expect(useMudStore.getState().worlds['local']).toBeUndefined();
  });

  it('updateStatus for "local" does not add "local" to worldOrder', () => {
    useMudStore.getState().updateStatus(makeStatusEvent('local', true));
    expect(useMudStore.getState().worldOrder).not.toContain('local');
  });

  it('lines for "local" are still stored for error display', () => {
    useMudStore.getState().appendLine(makeLineEvent('local', 'Error: already connected'));
    // "local" lines stored internally but world not in worldOrder/activeWorld
    expect(useMudStore.getState().worlds['local']?.lines[0]?.Text).toBe('Error: already connected');
  });
});

// ── C4: worldsStatus hydration must not create new tabs ───────────────────────

describe('C4 — worldsStatus hydration: update existing worlds only', () => {
  it('updateStatus on unknown world does not add it to worldOrder', () => {
    // Simulate worldsStatus returning a stale world the user never opened
    useMudStore.getState().updateStatus(makeStatusEvent('localhost', true));
    expect(useMudStore.getState().worldOrder).not.toContain('localhost');
    expect(useMudStore.getState().activeWorld).toBeNull();
  });

  it('updateStatus on an existing world updates its status', () => {
    useMudStore.getState().ensureWorld('aardmud.org');
    useMudStore.getState().updateStatus(makeStatusEvent('aardmud.org', true));
    const world = useMudStore.getState().worlds['aardmud.org'];
    expect(world?.connected).toBe(true);
    expect(world?.statusReceived).toBe(true);
  });
});

// ── C1: activeWorld tracks user intent ────────────────────────────────────────

describe('C1 — activeWorld set by first ensureWorld, not overwritten by second', () => {
  it('second ensureWorld does not change activeWorld', () => {
    useMudStore.getState().ensureWorld('world-a');
    useMudStore.getState().ensureWorld('world-b');
    expect(useMudStore.getState().activeWorld).toBe('world-a');
  });

  it('setActiveWorld overrides activeWorld explicitly', () => {
    useMudStore.getState().ensureWorld('world-a');
    useMudStore.getState().ensureWorld('world-b');
    useMudStore.getState().setActiveWorld('world-b');
    expect(useMudStore.getState().activeWorld).toBe('world-b');
  });
});

// ── H3: removeWorld ───────────────────────────────────────────────────────────

describe('H3 — removeWorld updates activeWorld and worldOrder correctly', () => {
  it('removing the active world makes the next tab active', () => {
    useMudStore.getState().ensureWorld('a');
    useMudStore.getState().ensureWorld('b');
    useMudStore.getState().ensureWorld('c');
    useMudStore.getState().setActiveWorld('b');
    useMudStore.getState().removeWorld('b');
    // After removing b, active should be a (first remaining) or c
    const { activeWorld, worldOrder } = useMudStore.getState();
    expect(worldOrder).not.toContain('b');
    expect(activeWorld).not.toBe('b');
    expect(activeWorld).not.toBeNull();
  });

  it('removing the only world sets activeWorld to null', () => {
    useMudStore.getState().ensureWorld('solo');
    useMudStore.getState().removeWorld('solo');
    expect(useMudStore.getState().activeWorld).toBeNull();
    expect(useMudStore.getState().worldOrder).toHaveLength(0);
  });

  it('removing a non-active world does not change activeWorld', () => {
    useMudStore.getState().ensureWorld('a');
    useMudStore.getState().ensureWorld('b');
    useMudStore.getState().setActiveWorld('a');
    useMudStore.getState().removeWorld('b');
    expect(useMudStore.getState().activeWorld).toBe('a');
  });
});

// ── Gagged lines ──────────────────────────────────────────────────────────────

describe('appendLine — gagged lines are dropped', () => {
  it('gagged event is not stored', () => {
    useMudStore.getState().ensureWorld('mud');
    useMudStore.getState().appendLine({ ...makeLineEvent('mud'), Gagged: true });
    expect(useMudStore.getState().worlds['mud'].lines).toHaveLength(0);
  });
});

// ── Scrollback limits ─────────────────────────────────────────────────────────

describe('appendLine — scrollback limit of 10,000 is enforced', () => {
  it('caps the scrollback lines at MAX_SCROLLBACK', () => {
    useMudStore.getState().ensureWorld('mud');
    const state = useMudStore.getState();
    // Append 10,005 lines
    for (let i = 0; i < 10005; i++) {
      state.appendLine(makeLineEvent('mud', `line ${i}`));
    }
    const lines = useMudStore.getState().worlds['mud'].lines;
    expect(lines).toHaveLength(10000);
    expect(lines[0].Text).toBe('line 5');
    expect(lines[9999].Text).toBe('line 10004');
  });
});
