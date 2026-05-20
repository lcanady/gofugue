# gofugue

A terminal MUD client written in Go — the spiritual successor to TinyFugue.

## Features

- **TinyFugue-compatible** — loads `.tf` scripts (`/def`, `/gag`, `/hilite`, `/substitute`, capture groups, `-E{expr}` conditions, one-shot/fallthrough/probability triggers)
- **Multiple transports** — `mud://` (plain TCP), `muds://` (TLS), `ws://`/`wss://` (WebSocket), `wt://` (WebTransport/QUIC)
- **Telnet support** — full IAC FSM, GMCP, CHARSET negotiation, Latin-1 transcoding
- **Scripting** — JavaScript (embedded via goja) and Python (subprocess bridge)
- **Timer system** — `/repeat`, `/cancel`, `/ps`; JS `tf.setTimeout`/`tf.setInterval`
- **Expression engine** — arithmetic, string functions, `{expr}` expansion, `$variables`
- **IPC server** — Unix socket + optional TCP/WebSocket JSON-RPC for external tools
- **TUI** — scrollback, mouse scroll, word-wrap, truecolor, tab completion, `[MORE]` indicator
- **Headless mode** — run without TUI, driven entirely via IPC

## Installation

### Pre-built binaries

Download the binary for your platform from the [releases page](https://github.com/kumakun/gofugue/releases):

```
gf-linux-amd64
gf-linux-arm64
gf-darwin-amd64
gf-darwin-arm64
gf-windows-amd64.exe
```

### Build from source

Requires Go 1.21+.

```sh
git clone https://github.com/kumakun/gofugue
cd gofugue
make build        # → bin/gf
make release      # → dist/ (all platforms)
```

## Quick start

```sh
make build        # compiles → bin/gf

# Connect directly
./bin/gf --connect mud://mymud.org:4000

# Load a TinyFugue script on startup
./bin/gf --script ~/.tf/mymud.tf

# Connect to a world saved in config
./bin/gf
# then: /connect mymud
```

Optionally install to `$PATH`:

```sh
go install github.com/kumakun/gofugue/cmd/gf@latest
# then: gf --connect mud://mymud.org:4000
```

## Configuration

Config file: `~/.config/gofugue/config.toml`

```toml
default_world    = "mymud"
startup_script   = "~/.tf/mymud.tf"
scrollback_lines = 10000

[ipc]
tcp_port = 7878
ws_port  = 7879

[theme]
# Status bar and input bar colours.
# By default no background colour is set — both bars inherit your terminal's
# own background.  Any colour must be chosen explicitly; nothing is assumed.
# Colour names: "navy", "white", "black", "red", "green", "yellow", "blue",
# "magenta", "cyan", "silver", "gray", "maroon", … (empty = terminal default)
status_fg      = ""      # foreground; empty = terminal default
status_bg      = ""      # background; empty = terminal default
status_bold    = false   # bold text (off by default)
status_reverse = false   # reverse fg/bg (off by default)

input_fg      = ""
input_bg      = ""
input_bold    = false
input_reverse = false

[world.mymud]
url     = "mud://mymud.org:4000"
char    = "MyChar"
pass    = "hunter2"
charset = "latin-1"

[world.sandbox]
url            = "muds://sandbox.example.com:4001"
tls_skip_verify = true
```

Example — classic navy status bar:

```toml
[theme]
status_fg      = "white"
status_bg      = "navy"
status_bold    = true
```

Example — reverse-video bars (swap terminal fg/bg):

```toml
[theme]
status_reverse = true
input_reverse  = true
status_bold    = false
```

## Commands

| Command | Description |
|---|---|
| `/connect <url> [name]` | Dial a world (`mud://`, `muds://`, `ws://`, `wss://`, `wt://`) |
| `/dc [name]` | Disconnect (defaults to foreground world) |
| `/sw <name>` | Switch foreground world |
| `/send <text>` | Send text to current world |
| `/echo <text>` | Print to local output |
| `/def [-t pat] [-p N] [-c N] [-n N] [-F] [-i] [-m mode] name=body` | Define a macro/trigger |
| `/gag [-t pattern] name=` | Suppress matching lines |
| `/hilite [-t pattern] name=colour` | Highlight matching lines |
| `/substitute [-t pattern] name=replacement` | Rewrite matching lines |
| `/undef <name>` | Remove a macro |
| `/list` | List all macros |
| `/set name=value` | Set a variable |
| `/unset <name>` | Unset a variable |
| `/listvar [pattern]` | List variables |
| `/log [file]` | Start logging to file; `/log` alone stops logging |
| `/recall [pattern]` | Search scrollback history |
| `/load <file>` | Import a `.tf` script |
| `/save [file]` | Save macros to JSON (default `macros.json`) |
| `/addworld <name> <url> [char [pass]]` | Register a world |
| `/listworlds` | List registered worlds |
| `/saveworld [file]` | Save world definitions to TOML |
| `/unworld <name>` | Remove a world |
| `/repeat <duration> <body>` | Fire body every interval (`5s`, `500ms`, `1m`) |
| `/cancel <id>` | Cancel a timer by ID |
| `/ps` | List active timers |
| `/restrict SHELL\|FILE\|WORLD` | Restrict capabilities |
| `/js <file>` | Load a JavaScript script |
| `/js -e <expr>` | Evaluate a JavaScript expression |
| `/py <file>` | Load a Python script (requires `--bridge-py`) |
| `/quit` | Exit |

## Scripting

### JavaScript

```js
// mymud.js
tf.on("GMCP:Char.Vitals", function(world, data) {
    tf.echo("HP: " + data.hp + "/" + data.maxhp);
});

tf.def("auto_heal", {trigger: "You are hungry"}, function() {
    tf.send("eat bread");
});

tf.setInterval(60000, function() {
    tf.send("score");
});
```

Load with `/js mymud.js` or `--script mymud.js`.

### Python

```python
# mymud.py
import tf

@tf.trigger(r"You are hungry")
def on_hungry(match):
    tf.send("eat bread")

@tf.on("GMCP", module="Char.Vitals")
def on_vitals(data):
    tf.echo(f"HP: {data['hp']}/{data['maxhp']}")
```

Load with `/py mymud.py` (requires `--bridge-py path/to/tf-lib/bridge.py`).

## Flags

| Flag | Default | Description |
|---|---|---|
| `--config <path>` | `~/.config/gofugue/config.toml` | Config file path |
| `--connect <url>` | — | Connect to URL on startup |
| `--script <file>` | — | Load `.tf` script on startup |
| `--bridge-py <path>` | — | Path to `tf-lib/bridge.py` (enables `/py`) |
| `--headless` | false | Run without TUI (IPC-only) |
| `--ipc-port <N>` | 0 | TCP IPC port (overrides config) |
| `--ipc-ws-port <N>` | 0 | WebSocket IPC port (overrides config) |
| `--version` | — | Print version and exit |

## TUI layout

```
┌─────────────────────────────────────────┐
│  Output pane  (scrollable)              │
│─────────────────────────────────────────│  ← border
│  GoFugue │ mymud │ CONNECTED │ 07:42:01 │  ← status bar
│─────────────────────────────────────────│  ← border
│  > _                                    │  ← input bar
└─────────────────────────────────────────┘
```

The input bar shows a plain `> ` prompt. The active world name is shown in the
status bar only — it is not repeated in the input field.

## Key bindings

| Key | Action |
|---|---|
| `Enter` | Send input |
| `Tab` | Complete command / macro / world name |
| `↑` / `↓` | Input history |
| `PgUp` / `PgDn` | Scroll output |
| `Mouse wheel` | Scroll output |
| `Ctrl+C` | Quit |

## TinyFugue compatibility

gofugue loads `.tf` scripts directly. Supported directives:

- `/def` with flags: `-t` (trigger), `-p` (priority), `-c` (probability), `-n` (shots), `-F` (fallthrough), `-i` (invisible), `-E` (disabled), `-w` (world), `-m` (match mode), `-h` (hook)
- `/gag`, `/hilite`, `/substitute`
- `/set`, `/key`
- Capture groups `%1`–`%9` in trigger bodies
- `{expr}` blocks and `$variable` expansion
- `-E{expr}` condition gates (evaluated at fire time)
- Match modes: `regexp` (default), `glob`, `substr`

## Building

```sh
make build        # local binary → bin/gf
make test         # go test -race ./...
make vet          # go vet ./...
make lint         # golangci-lint run ./...
make release      # cross-compile all platforms → dist/
make clean        # remove bin/ and dist/
```
