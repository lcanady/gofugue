# Command Reference

All commands begin with `/`. Type them in the input bar or include them in scripts.  
Tab completion works for command names.

---

## Connection

### `/connect <url> [name]`
Dial a world. The optional `name` labels it; defaults to the host.

```
/connect mud://mymud.org:4000
/connect mud://mymud.org:4000 mymud
/connect mymud                         ← connects a saved world by name
```

Supported URL schemes:

| Scheme | Transport |
|---|---|
| `mud://host:port` | Plain TCP |
| `muds://host:port` | TLS |
| `ws://host:port/path` | WebSocket |
| `wss://host:port/path` | WebSocket over TLS |
| `wt://host:port/path` | WebTransport (QUIC) |

### `/dc [name]`
Disconnect from a world. Defaults to the foreground world.

```
/dc
/dc mymud
```

### `/sw <name>`
Switch the foreground world (the one that receives your input).

```
/sw mymud
/sw testbed
```

---

## Output

### `/echo <text>`
Print text to the local output pane. Supports `{expr}` and `$variable` expansion.

```
/echo Hello, world!
/echo HP is $hp
```

### `/send <text>`
Send text directly to the current world, bypassing aliases.

```
/send look
/send say Hello
```

### `/recall [pattern]`
Search scrollback history. Pattern is a substring match.

```
/recall
/recall dragon
```

### `/log [file]`
Start logging output to `file`. Call `/log` with no argument to stop.

```
/log ~/session.log
/log
```

---

## Macros & Triggers

### `/def [flags] name=body`
Define a macro, trigger, alias, or hook handler.

**Flags:**

| Flag | Description |
|---|---|
| `-t <pattern>` | Trigger pattern (fires when MUD output matches) |
| `-p <N>` | Priority (higher fires first; default 0) |
| `-c <N>` | Probability 0–100 (fires N% of the time) |
| `-n <N>` | One-shot: fire N times then auto-remove |
| `-F` | Fallthrough — don't stop after this trigger fires |
| `-i` | Invisible — don't echo the trigger body |
| `-m <mode>` | Match mode: `regexp` (default), `glob`, `substr` |
| `-w <world>` | Bind to a specific world only |
| `-h <hook>` | Hook name: `CONNECT`, `DISCONNECT`, `PROMPT`, etc. |
| `-E{expr}` | Condition gate — only fire if expression is truthy |

```
/def -t"You are hungry" eat_bread=eat bread
/def -t"^HP: (\d+)" -p10 track_hp=;set hp=%1
/def -n1 once=echo This fires once
/def -h CONNECT greet=say Hello, world!
/def -t"^> $" -m substr show_prompt=echo Got prompt
```

Capture groups `%1`–`%9` are available in the body.

### `/gag [-t pattern] name=`
Suppress lines matching the pattern from the output pane.

```
/gag -t"^You feel slightly dizzy" gag_dizzy=
```

### `/hilite [-t pattern] name=colour`
Highlight matching lines in a colour.

```
/hilite -t"your name" hi_name=red
/hilite -t"DANGER" hi_danger=bold red
```

### `/substitute [-t pattern] name=replacement`
Rewrite matching lines before display.

```
/substitute -t"u r" fix_ur=you are
```

### `/undef <name>`
Remove a macro by name.

```
/undef eat_bread
```

### `/list`
List all defined macros with their flags and bodies.

---

## Variables

### `/set name=value`
Set a variable. Accessible as `$name` or `{name}` in expressions.

```
/set hp=100
/set myname=Hero
```

### `/unset <name>`
Remove a variable.

### `/listvar [pattern]`
List all variables, optionally filtered by pattern.

```
/listvar
/listvar hp
```

---

## Worlds

### `/addworld <name> <url> [char [pass]]`
Save a world profile to the in-memory registry.

```
/addworld mymud mud://mymud.org:4000 Hero hunter2
```

### `/listworlds`
List all registered worlds.

### `/saveworld [file]`
Save world definitions to TOML (default: `worlds.toml`).

### `/unworld <name>`
Remove a world from the registry.

---

## Timers

### `/repeat <duration> <body>`
Fire `body` repeatedly at `duration` intervals.  
Duration format: `5s`, `500ms`, `1m`, `1m30s`.

```
/repeat 60s score
/repeat 5s echo tick
```

Returns a timer ID printed to output.

### `/cancel <id>`
Cancel a timer by ID.

```
/cancel 3
```

### `/ps`
List all active timers with their IDs and intervals.

---

## Scripting

### `/js <file>`
Load and execute a JavaScript file.

```
/js ~/.config/gofugue/mymud.js
```

### `/js -e <expr>`
Evaluate a JavaScript expression inline.

```
/js -e tf.echo("hello from JS")
```

### `/py <file>`
Load a Python script (requires `--bridge-py`).

```
/py ~/.config/gofugue/mymud.py
```

---

## System

### `/load <file>`
Import a `.tf` TinyFugue-compatible script.

```
/load ~/.tf/mymud.tf
```

### `/save [file]`
Save all macros to JSON (default: `macros.json`).

### `/restrict SHELL|FILE|WORLD`
Disable a capability class.

| Value | Effect |
|---|---|
| `SHELL` | Disables shell execution from scripts |
| `FILE` | Disables file read/write from scripts |
| `WORLD` | Disables new world connections |

### `/help`
Print a summary of available commands.

### `/quit`
Exit gf.
