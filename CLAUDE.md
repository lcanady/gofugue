# gofugue — Claude guide

This file orients Claude Code for work in this repo. Read it before writing any code.

---

## What the project is

`gofugue` is a terminal MUD client written in Go. It is a spiritual successor to TinyFugue (TF).
The binary runs either with a built-in TUI (`tcell`) or in `--headless` mode, where the TUI
is replaced by a JSON-RPC IPC server that external frontends connect to.

Primary working directory: `github.com/kumakun/gofugue`  
Main binary entry point: `cmd/gf/main.go`

---

## Architecture overview

```
                    ┌──────────────────────────────────┐
                    │          bus.Bus                 │
                    │  (typed fan-out event dispatcher)│
                    └──┬───────┬───────┬───────┬───────┘
                       │       │       │       │
              world    │  cmd  │  TUI  │  IPC  │  script
              manager  │  disp │       │  srv  │  bridges
```

All components communicate exclusively through `internal/bus`. No direct cross-package
function calls at runtime. The bus is the single source of truth for event flow.

### Key packages

| Path | Role |
|---|---|
| `internal/bus` | Typed event bus (fan-out, non-blocking drop on slow subs) |
| `internal/world` | World manager: connects, disconnects, reads, writes MUD bytes |
| `internal/macro` | Trigger/alias/hook engine |
| `internal/cmd` | `/command` dispatcher; all builtins live here |
| `internal/tui` | `tcell`-based TUI; subscribes to `EvWorldLineRendered` |
| `internal/ipc` | JSON-RPC 2.0 IPC server (Unix sock + TCP + WebSocket) |
| `internal/expr` | Expression evaluator (`{expr}`, `$var`) |
| `internal/history` | Scrollback buffer per world |
| `internal/timers` | Goroutine-pool timer system |
| `internal/proto` | Telnet FSM + GMCP + CHARSET |
| `internal/compat` | TinyFugue `.tf` script parser |
| `internal/script/js` | JavaScript bridge (goja embedded VM) |
| `internal/script/py` | Python bridge (subprocess + JSON stdio) |
| `internal/transport` | Dial abstraction: TCP, TLS, WebSocket, WebTransport |
| `internal/config` | TOML config loader |
| `internal/ansi` | ANSI escape parser → `bus.LineAttrs` |

---

## Bus event reference

All events are defined in `internal/bus/bus.go`.

### Event types (string constants)

| Constant | String value | Direction | Description |
|---|---|---|---|
| `EvWorldLine` | `"world.line"` | world→client | Raw line from MUD, pre-pipeline |
| `EvWorldLineRendered` | `"world.line.rendered"` | pipeline→TUI/IPC | After gag/hilite/substitute |
| `EvGMCP` | `"gmcp"` | world→client | GMCP package from MUD |
| `EvHook` | `"hook"` | various | Lifecycle hooks (CONNECT, DISCONNECT, …) |
| `EvStatus` | `"status"` | world→client | Connection state / lag |
| `EvUserInput` | `"user.input"` | frontend→pipeline | Raw text sent by user |
| `EvUserCmd` | `"user.cmd"` | frontend→pipeline | `/command` line from user |

### Event struct shapes (as JSON, received over IPC)

**`world.line` / `world.line.rendered`**
```json
{
  "WorldName": "mymud",
  "Text":      "You are hungry.",
  "Attrs":     {"FG": -1, "BG": -1, "Bold": false, "Underline": false, "Italic": false, "Reverse": false, "FGRGB": [0,0,0], "BGRGB": [0,0,0]},
  "Spans":     [{"Text": "You are ", "Attrs": {...}}, {"Text": "hungry", "Attrs": {"FG": 1, ...}}],
  "Gagged":    false
}
```
`Spans` is non-empty only when the line has mid-line colour changes. `Text` is always the
plain (ANSI-stripped) full line. `Attrs.FG`/`BG` are palette indices (-1 = default, -2 = use RGB).

**`gmcp`**
```json
{"WorldName": "mymud", "Module": "Char.Vitals", "Data": {"hp": 100, "maxhp": 100}}
```
`Data` is the raw JSON of the GMCP package. It arrives pre-parsed (not a string).

**`hook`**
```json
{"WorldName": "mymud", "Name": "CONNECT"}
```
Hook names: `CONNECT`, `DISCONNECT`, `PROMPT`, `SEND`, `ACTIVITY`, `RESIZE`, `QUIT`.

**`status`**
```json
{"WorldName": "mymud", "Connected": true, "LagMS": 42}
```

**`user.input`** / **`user.cmd`**  
These flow *from* frontend *to* the pipeline. You won't normally receive them unless you also
subscribed to them (useful for IPC relay/logging tools).

---

## IPC protocol

The IPC server speaks **newline-delimited JSON-RPC 2.0** over three simultaneous listeners:

| Transport | Default address | Good for |
|---|---|---|
| Unix socket | `~/.config/gofugue/gofugue.sock` | Same-host daemons, CLI tools |
| TCP | `127.0.0.1:7878` | GUI frontends, Tcl/Tk, Windows |
| WebSocket | `ws://127.0.0.1:7879/` | Electron, React, browser-based UIs |

All three use **identical** framing and message shapes. The WebSocket path strips/adds the
trailing newline automatically in the server adapter (`internal/ipc/ws.go`).

### Framing

**TCP / Unix socket:** each message is one JSON object followed by `\n`.  
**WebSocket:** each message is one JSON text frame (no trailing newline needed from the client).

### Request format

```json
{"jsonrpc": "2.0", "id": 1, "method": "subscribe", "params": {"events": ["world.line.rendered"]}}
```

`id` must be present for calls (omit for notifications — server won't respond).

### Response format

```json
{"jsonrpc": "2.0", "id": 1, "result": {"ok": true}}
{"jsonrpc": "2.0", "id": 1, "error": {"code": -32601, "message": "method not found: foo"}}
```

### Server-push notification format

```json
{"jsonrpc": "2.0", "method": "world.line.rendered", "params": { ... event fields ... }}
```

`method` is the event type string. `params` is the event struct serialised as JSON.
**There is no `id` field.** This distinguishes notifications from responses.

---

## Built-in IPC methods

### `subscribe`

Subscribe to one or more event types. Must be called before events arrive.

```json
{"jsonrpc":"2.0","id":1,"method":"subscribe","params":{"events":["world.line.rendered","hook","status"]}}
```

Pass an empty array `[]` to subscribe to **all** events.

Response: `{"result": {"ok": true}}`

Subscriptions are additive and permanent for the lifetime of the connection.
There is no `unsubscribe`.

### `history.get`

Fetch the last N lines from the combined scrollback of all worlds.

```json
{"jsonrpc":"2.0","id":2,"method":"history.get","params":{"n":100}}
```

Response: `{"result": ["line1", "line2", ...]}`  
Lines are plain text (ANSI stripped), oldest first.

### `input`

Send raw text to a world. Goes through the alias/speedwalk pipeline exactly as if the user
typed it in the TUI input box.

```json
{"jsonrpc":"2.0","id":3,"method":"input","params":{"world":"mymud","text":"look"}}
```

`world` is optional — defaults to the current foreground world.  
Response: `{"result": {"ok": true}}`

### `cmd`

Dispatch a `/command` line. Equivalent to typing `/connect mud://…` in the TUI.

```json
{"jsonrpc":"2.0","id":4,"method":"cmd","params":{"world":"mymud","line":"/connect mud://mymud.org:4000"}}
```

`line` must start with `/`.  
`world` is optional — defaults to the current foreground world.  
Response: `{"result": {"ok": true}}`

---

## Typical interface session flow

```
1. Connect to IPC (TCP or WebSocket)
2. Send subscribe request for desired events
3. (optional) Send history.get to populate initial scrollback
4. (optional) Send cmd to connect a world:
     {"method":"cmd","params":{"line":"/connect mud://mymud.org:4000"}}
5. Receive hook notification: {"method":"hook","params":{"WorldName":"mymud","Name":"CONNECT"}}
6. Receive world.line.rendered notifications for every MUD line
7. Send input to send text to the MUD:
     {"method":"input","params":{"text":"look"}}
8. On quit, close the connection
```

---

## Writing a new interface: step-by-step

### 1. Choose your transport

- **Same-machine app** (Go, Python, Rust CLI): Unix socket — `~/.config/gofugue/gofugue.sock`
- **GUI app (Electron, Qt, native)**: TCP `127.0.0.1:7878`
- **Web-based / React / browser**: WebSocket `ws://127.0.0.1:7879/`

The config can override ports:
```toml
[ipc]
tcp_port = 7878
ws_port  = 7879
```
Or pass `--ipc-port` / `--ipc-ws-port` on the command line.

### 2. Decide which events you need

For a full display terminal:
```json
{"events": ["world.line.rendered", "hook", "status", "gmcp"]}
```

For a minimal "just show lines" overlay:
```json
{"events": ["world.line.rendered"]}
```

For a GMCP data panel only:
```json
{"events": ["gmcp"]}
```

### 3. Handle `world.line.rendered` for display

```
params.Text        — plain text, always set
params.Spans       — non-empty for lines with mid-line colour; each span has Text + Attrs
params.Attrs       — whole-line colour if Spans is empty
params.Gagged      — skip this line if true
params.WorldName   — which world sent it; "local" = gofugue itself (echo, errors)
```

`Attrs.FG` / `Attrs.BG` values:
- `-1` = terminal default
- `0–7` = standard palette (black, red, green, yellow, blue, magenta, cyan, white)
- `8–15` = bright variants
- `-2` = use `FGRGB` / `BGRGB` true-colour fields

### 4. Handle `hook` for connection state

```
Name == "CONNECT"     → world connected; safe to start sending
Name == "DISCONNECT"  → world disconnected
Name == "PROMPT"      → partial-line prompt detected (useful for prompt display)
Name == "QUIT"        → gofugue is shutting down; close your connection
```

### 5. Handle `status` for lag display

```
params.Connected  — bool
params.LagMS      — round-trip milliseconds to MUD; update a status bar
```

### 6. Handle `gmcp` for structured game data

```
params.Module  — e.g. "Char.Vitals", "Room.Info", "Comm.Channel"
params.Data    — parsed JSON (already an object, not a string)
```

### 7. Adding custom IPC methods (Go)

If you need a new RPC method accessible from the frontend, register it on the `ipcServer`
in `cmd/gf/main.go` before calling `ipcServer.Run`:

```go
ipcServer.Handle("worlds.list", func(_ context.Context, _ *ipc.Client, _ json.RawMessage) (any, error) {
    return worldMgr.WorldInfos(), nil
})
```

The handler signature is `func(ctx context.Context, client *ipc.Client, params json.RawMessage) (any, error)`.
Return `(nil, error)` to send a JSON-RPC error. Return `(value, nil)` to send a result.

---

## Worked example: Python CLI watcher

```python
import socket, json, threading

sock = socket.socket(socket.AF_UNIX)
sock.connect("/Users/you/.config/gofugue/gofugue.sock")

def send(msg):
    sock.sendall((json.dumps(msg) + "\n").encode())

def recv_loop():
    buf = b""
    while True:
        buf += sock.recv(4096)
        while b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            msg = json.loads(line)
            if "method" in msg and "id" not in msg:
                on_notification(msg)

def on_notification(msg):
    method = msg["method"]
    params = msg["params"]
    if method == "world.line.rendered" and not params.get("Gagged"):
        print(f"[{params['WorldName']}] {params['Text']}")
    elif method == "hook":
        print(f"*** {params['Name']} {params['WorldName']}")
    elif method == "status":
        state = "CONNECTED" if params["Connected"] else "DISCONNECTED"
        print(f"[{params['WorldName']}] {state} lag={params['LagMS']}ms")

threading.Thread(target=recv_loop, daemon=True).start()

# Subscribe
send({"jsonrpc":"2.0","id":1,"method":"subscribe",
      "params":{"events":["world.line.rendered","hook","status"]}})

# Connect a world
send({"jsonrpc":"2.0","id":2,"method":"cmd",
      "params":{"line":"/connect mud://mymud.org:4000"}})

# Keep alive
input()
```

---

## Worked example: JavaScript/Node.js watcher

```js
const WebSocket = require('ws');
const ws = new WebSocket('ws://127.0.0.1:7879/');
let id = 0;
const send = (msg) => ws.send(JSON.stringify(msg));

ws.on('open', () => {
  send({ jsonrpc:'2.0', id:++id, method:'subscribe',
         params:{ events:['world.line.rendered','hook','status','gmcp'] } });
  send({ jsonrpc:'2.0', id:++id, method:'history.get', params:{ n:50 } });
});

ws.on('message', (raw) => {
  const msg = JSON.parse(raw);
  if ('result' in msg || 'error' in msg) return; // response to our call

  const { method, params } = msg;
  if (method === 'world.line.rendered' && !params.Gagged) {
    console.log(`[${params.WorldName}] ${params.Text}`);
  } else if (method === 'hook') {
    console.log(`*** ${params.Name} ${params.WorldName}`);
  } else if (method === 'gmcp') {
    console.log(`GMCP ${params.Module}:`, params.Data);
  }
});

// Send text to the MUD
function sendToMud(text) {
  send({ jsonrpc:'2.0', id:++id, method:'input', params:{ text } });
}
```

---

## Worked example: React hook

```ts
// useMUD.ts
import { useEffect, useRef, useState } from 'react';

interface Line { world: string; text: string; spans: Span[]; gagged: boolean }
interface Span { Text: string; Attrs: LineAttrs }
interface LineAttrs { FG: number; BG: number; Bold: boolean; Underline: boolean }

export function useMUD(wsUrl = 'ws://127.0.0.1:7879/') {
  const ws = useRef<WebSocket | null>(null);
  const idRef = useRef(0);
  const [lines, setLines] = useState<Line[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const socket = new WebSocket(wsUrl);
    ws.current = socket;

    const send = (msg: object) => socket.send(JSON.stringify(msg));

    socket.onopen = () => {
      send({ jsonrpc:'2.0', id:++idRef.current, method:'subscribe',
             params:{ events:['world.line.rendered','hook','status'] } });
    };

    socket.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      if ('result' in msg || 'error' in msg) return;
      const { method, params } = msg;
      if (method === 'world.line.rendered' && !params.Gagged) {
        setLines(prev => [...prev.slice(-5000), { world: params.WorldName, text: params.Text, spans: params.Spans ?? [], gagged: false }]);
      } else if (method === 'status') {
        setConnected(params.Connected);
      }
    };

    return () => socket.close();
  }, [wsUrl]);

  const sendInput = (text: string) => ws.current?.send(JSON.stringify(
    { jsonrpc:'2.0', id:++idRef.current, method:'input', params:{ text } }
  ));

  const sendCmd = (line: string) => ws.current?.send(JSON.stringify(
    { jsonrpc:'2.0', id:++idRef.current, method:'cmd', params:{ line } }
  ));

  return { lines, connected, sendInput, sendCmd };
}
```

---

## Running in headless mode

```sh
# Start gf without TUI; IPC on defaults (socket + TCP 7878 + WS 7879)
gf --headless

# Override ports
gf --headless --ipc-port 8000 --ipc-ws-port 8001

# With a startup script and auto-connect
gf --headless --script ~/mymud.tf --connect mud://mymud.org:4000
```

When headless, gofugue:
- Runs all pipelines (triggers, timers, macros, GMCP, scripting) normally
- Publishes all the same events the TUI would see
- Blocks on `<-ctx.Done()` instead of running `tui.Run`
- The IPC server is the only user-facing interface

---

## What gofugue does NOT do over IPC (known gaps)

- **No cursor/prompt tracking** — `PROMPT` hook fires but gofugue doesn't tell you the byte
  offset where the partial line starts. The frontend must decide where to draw a prompt.
- **No worlds.list method** — there's no built-in RPC to enumerate connected worlds. Add one
  via `ipcServer.Handle("worlds.list", ...)` in `main.go` if needed.
- **No per-world scrollback retrieval** — `history.get` returns lines from all worlds merged.
  Add a `world` filter parameter if you need per-world history.
- **No TLS on IPC listeners** — all three listeners are plaintext and loopback-only by design.
  Do not expose them to the network.

---

## TUI internals

The TUI (`internal/tui/`) has three panes drawn on every event:

```
OutputPane  rows 0 .. h-3   scrollable world output; subscribes to EvWorldLineRendered
StatusBar   row h-2          one-line summary: app│world│conn│lag│time
InputBar    row h-1          readline editor; always shows "> " prompt
```

**Input bar prompt**: the prompt is always `"> "`. The current world name is shown in the
status bar only — it is intentionally NOT repeated in the input field. Do not add world-name
logic to `InputBar.SetPrompt` or the status event handler in `RunWithScreen`.

**Theming**: both bars accept a `tcell.Style` via `SetStyle`. Styles are built in
`cmd/gf/main.go:themeStyle()` from `config.ThemeConfig` fields before `app.Run` is
called. The hard rule: **both bars default to `tcell.StyleDefault` — no colour, no bold,
no reverse, nothing.** The terminal renders them exactly like any other text row. Any
styling whatsoever (including `Bold` or `Reverse`) must come from explicit user config in
`[theme]`. To change the built-in default, update both `config.Defaults()` and `tui.New()`.

Colour names in config are resolved via `tcell.ColorNames` (lowercase lookup). Unknown names
silently fall back to terminal default.

**slog during TUI**: slog is redirected to `~/<cache>/gofugue/gofugue.log` (or `io.Discard`
if that file can't be opened) before `app.Run`. Any slog call that reaches stderr while tcell
owns the terminal corrupts the display. Never add `slog.*` calls to paths that run during
TUI operation without verifying the redirect is in place.

---

## File layout cheatsheet

```
cmd/gf/main.go          — entry point; wires everything; IPC handlers + themeStyle() live here
internal/bus/bus.go          — all event types and structs
internal/ipc/ipc.go          — IPC server, handler registry, broadcast
internal/ipc/client.go       — per-client serve loop (JSON-RPC framing)
internal/ipc/ws.go           — WebSocket adapter + listener
internal/world/world.go      — world manager, read loop, auto-login
internal/cmd/cmd.go          — /command dispatcher + all builtin commands
internal/macro/macro.go      — trigger/hook/alias engine
internal/config/config.go    — TOML config structs + loader (includes ThemeConfig)
internal/tui/tui.go          — App: event loop, SetTheme, SetOnReady, initLayout
internal/tui/status.go       — StatusBar: pluggable fields, SetStyle
internal/tui/input.go        — InputBar: readline editor, SetStyle
internal/tui/output.go       — OutputPane: scrollback, word-wrap, Draw
internal/tui/styles.go       — attrToStyle(), fromWorldLine(), LogicalLine
```
