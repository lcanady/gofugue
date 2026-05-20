# Configuration

Config file location: `~/.config/gofugue/config.toml`  
(Respects `$XDG_CONFIG_HOME` — override with `--config <path>`)

A missing config file is silently ignored; all values fall back to defaults.

---

## Top-level keys

| Key | Type | Default | Description |
|---|---|---|---|
| `default_world` | string | `""` | World name to connect on startup |
| `startup_script` | string | `""` | `.tf` or `.js` script to load on startup |
| `scrollback_lines` | int | `5000` | Per-world scrollback buffer depth |
| `wrap_width` | int | `0` | Line-wrap column; `0` = terminal width |

---

## `[ipc]`

Controls the JSON-RPC IPC server. See [ipc.md](ipc.md) for the full protocol.

| Key | Type | Default | Description |
|---|---|---|---|
| `socket_path` | string | `~/.config/gofugue/gofugue.sock` | Unix domain socket path |
| `tcp_port` | int | `7878` | TCP listener port; `0` = disabled |
| `ws_port` | int | `7879` | WebSocket listener port; `0` = disabled |

```toml
[ipc]
tcp_port = 7878
ws_port  = 7879
```

---

## `[theme]`

Colours for the status bar and input bar. All values default to your terminal's
own colours — nothing is assumed. Set only what you want to change.

| Key | Type | Default | Description |
|---|---|---|---|
| `status_fg` | string | `""` | Status bar foreground colour |
| `status_bg` | string | `""` | Status bar background colour |
| `status_bold` | bool | `false` | Bold text in status bar |
| `status_reverse` | bool | `false` | Reverse fg/bg in status bar |
| `input_fg` | string | `""` | Input bar foreground colour |
| `input_bg` | string | `""` | Input bar background colour |
| `input_bold` | bool | `false` | Bold text in input bar |
| `input_reverse` | bool | `false` | Reverse fg/bg in input bar |

**Colour names** (case-insensitive):  
`black`, `maroon`, `green`, `olive`, `navy`, `purple`, `teal`, `silver`,  
`gray`, `red`, `lime`, `yellow`, `blue`, `fuchsia`, `aqua`, `white`,  
and all standard extended terminal colour names recognised by `tcell`.  
An empty string means "use the terminal default."

### Examples

Classic navy status bar:
```toml
[theme]
status_fg   = "white"
status_bg   = "navy"
status_bold = true
```

Reverse-video bars (swaps terminal fg/bg):
```toml
[theme]
status_reverse = true
input_reverse  = true
```

Minimal — status bar bold only, no colour:
```toml
[theme]
status_bold = true
```

---

## `[world.<name>]`

Named world profiles. Connect with `/connect <name>` or set as `default_world`.

| Key | Type | Description |
|---|---|---|
| `url` | string | Transport URL — `mud://`, `muds://`, `ws://`, `wss://`, `wt://` |
| `char` | string | Character name (sent as part of auto-login) |
| `pass` | string | Password (stored in plaintext — consider your threat model) |
| `login` | string | Full auto-login string sent on `CONNECT` hook |
| `charset` | string | Character encoding: `utf-8` (default) or `latin-1` |
| `tls_skip_verify` | bool | Skip TLS certificate verification (muds:// / wss://) |
| `telnet_enabled` | bool | Force telnet IAC processing on/off; omit for auto-detect |

```toml
[world.mymud]
url     = "mud://mymud.org:4000"
char    = "Hero"
pass    = "hunter2"
charset = "latin-1"

[world.testbed]
url             = "muds://sandbox.example.com:4001"
tls_skip_verify = true
```

---

## Full example

```toml
default_world    = "mymud"
startup_script   = "~/.config/gofugue/mymud.tf"
scrollback_lines = 10000
wrap_width       = 0

[ipc]
tcp_port = 7878
ws_port  = 7879

[theme]
status_fg   = "white"
status_bg   = "navy"
status_bold = true

[world.mymud]
url     = "mud://mymud.org:4000"
char    = "Hero"
pass    = "hunter2"
charset = "latin-1"

[world.testbed]
url             = "muds://sandbox.example.com:4001"
tls_skip_verify = true
```
