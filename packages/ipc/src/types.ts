/**
 * Types for the gofugue JSON-RPC 2.0 IPC protocol.
 * All event shapes mirror the Go bus structs exactly.
 */

export type EventType =
  | 'world.line'
  | 'world.line.rendered'
  | 'gmcp'
  | 'hook'
  | 'status'
  | 'user.input'
  | 'user.cmd';

/** ANSI display attributes for a line or span. */
export interface LineAttrs {
  /** Palette index: -1 = terminal default, 0-15 = standard palette, -2 = use FGRGB */
  FG: number;
  /** Palette index: -1 = terminal default, 0-15 = standard palette, -2 = use BGRGB */
  BG: number;
  FGRGB: [number, number, number];
  BGRGB: [number, number, number];
  Bold: boolean;
  Underline: boolean;
  Italic: boolean;
  Reverse: boolean;
}

/** A styled run of text within a line. */
export interface Span {
  Text: string;
  Attrs: LineAttrs;
}

/** Raw or rendered line from a world connection. */
export interface WorldLineEvent {
  WorldName: string;
  /** Plain text (ANSI-stripped). Always set. */
  Text: string;
  /** Whole-line attrs (used when Spans is empty). */
  Attrs: LineAttrs;
  /** Non-empty when the line has mid-line colour changes. Null when no spans. */
  Spans: Span[] | null;
  /** True when a trigger has gagged (suppressed) this line. */
  Gagged: boolean;
}

/** GMCP package notification. Data is a pre-parsed object. */
export interface GMCPEvent {
  WorldName: string;
  /** e.g. "Char.Vitals", "Room.Info" */
  Module: string;
  Data: unknown;
}

/** Lifecycle hook notification. */
export interface HookEvent {
  WorldName: string;
  /** CONNECT | DISCONNECT | PROMPT | SEND | ACTIVITY | RESIZE | QUIT */
  Name: string;
}

/** Connection state update. */
export interface StatusEvent {
  WorldName: string;
  Connected: boolean;
  LagMS: number;
}

export type AnyEvent = WorldLineEvent | GMCPEvent | HookEvent | StatusEvent;

export interface JsonRpcRequest {
  jsonrpc: '2.0';
  id: number;
  method: string;
  params?: unknown;
}

export interface JsonRpcNotification {
  jsonrpc: '2.0';
  method: string;
  params: AnyEvent;
}

export interface JsonRpcResponse {
  jsonrpc: '2.0';
  id: number;
  result?: unknown;
  error?: { code: number; message: string };
}
