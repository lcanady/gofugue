/**
 * Factory helpers for gofugue IPC event payloads.
 * Shape mirrors the structs in internal/bus/bus.go and CLAUDE.md.
 */

export interface LineAttrs {
  FG: number;   // -1 = default, 0-15 = palette, -2 = use RGB
  BG: number;
  Bold: boolean;
  Underline: boolean;
  Italic: boolean;
  Reverse: boolean;
  FGRGB: [number, number, number];
  BGRGB: [number, number, number];
}

export interface Span {
  Text: string;
  Attrs: LineAttrs;
}

export interface WorldLineRenderedEvent {
  WorldName: string;
  Text: string;
  Attrs: LineAttrs;
  Spans: Span[];
  Gagged: boolean;
}

const defaultAttrs: LineAttrs = {
  FG: -1, BG: -1,
  Bold: false, Underline: false, Italic: false, Reverse: false,
  FGRGB: [0, 0, 0], BGRGB: [0, 0, 0],
};

/** Build a plain-text world.line.rendered event. */
export function line(
  text: string,
  worldName = 'testworld',
  overrides: Partial<WorldLineRenderedEvent> = {},
): WorldLineRenderedEvent {
  return {
    WorldName: worldName,
    Text: text,
    Attrs: defaultAttrs,
    Spans: [],
    Gagged: false,
    ...overrides,
  };
}

/** Build a coloured world.line.rendered event (mid-line colour changes). */
export function colouredLine(
  worldName: string,
  spans: Array<{ text: string; fg?: number; bold?: boolean }>,
): WorldLineRenderedEvent {
  const text = spans.map((s) => s.text).join('');
  return {
    WorldName: worldName,
    Text: text,
    Attrs: defaultAttrs,
    Spans: spans.map((s) => ({
      Text: s.text,
      Attrs: {
        ...defaultAttrs,
        FG: s.fg ?? -1,
        Bold: s.bold ?? false,
      },
    })),
    Gagged: false,
  };
}

/** A gagged line (should not appear in the terminal). */
export function gaggedLine(worldName = 'testworld'): WorldLineRenderedEvent {
  return line('this line should not appear', worldName, { Gagged: true });
}

/** CONNECT hook event. */
export function hookConnect(worldName: string) {
  return { WorldName: worldName, Name: 'CONNECT' };
}

/** DISCONNECT hook event. */
export function hookDisconnect(worldName: string) {
  return { WorldName: worldName, Name: 'DISCONNECT' };
}

/** status event. */
export function statusEvent(worldName: string, connected: boolean, lagMS = 0) {
  return { WorldName: worldName, Connected: connected, LagMS: lagMS };
}
