# gofugue completion roadmap

## Goal
Fix crashes, add timers, expose GMCP to scripts, fix Python, add charset support, polish TUI.

## Phase 1 — Blockers
- [x] 1. Replace `panic()` in `script/js/js.go` tf.send/tf.def with goja error returns → Verify: `go build` clean; bad JS args return error string, don't crash
- [x] 2. Fix `transport/webtransport.go` Dial() returning nil conn → Verify: `/connect wt://x` returns error message, no nil deref panic
- [x] 3. Fix `script/py/py.go` orphaned process: kill subprocess if encoder setup fails after `cmd.Start()` → Verify: forced encoder error leaves no zombie python3

## Phase 2 — Timer system
- [x] 4. Create `internal/timers/timers.go` — goroutine pool with `Add(id, delay, repeat, fn) Cancel(id) List() []TimerInfo` → Verify: unit tests pass
- [x] 5. Wire timers into JS bridge: `tf.setTimeout(ms,fn)`, `tf.setInterval(ms,fn)`, `tf.clearTimeout(id)`, `tf.clearInterval(id)` → Verify: `br.Eval("tf.setTimeout(10, function(){tf.echo('ok')})")` calls echo after 10ms
- [x] 6. Add `/repeat <duration> <body>` to `cmd/cmd.go` + Context.AddTimer/CancelTimer → Verify: `/repeat 1s /echo tick` fires every second; `/undef` cancels it
- [x] 7. Add `/ps` command listing active timers (id, interval, next-fire) → Verify: `/ps` after `/repeat` shows entry; after cancel, list is empty

## Phase 3 — GMCP in scripts
- [x] 8. JS bridge subscribes to `bus.EvGMCP`; delivers to `tf.on("GMCP:Char.Vitals", fn)` callbacks → Verify: publish GMCPEvent → JS fn called with (world, data)
- [x] 9. Python bridge forwards GMCP to subprocess as `{"t":"event","name":"GMCP","module":"...","data":{}}` → Verify: mock subprocess receives GMCP JSON

## Phase 4 — Python fixes
- [x] 10. Fix caps bug in `script/py/py.go:watchLines` — send separate TRIGGER event per matched trigger with its own captures → Verify: two patterns on same line each get correct caps
- [x] 11. Fix hot-reload: on second `Load()` call kill old subprocess, start fresh → Verify: `/py a.py` then edit+`/py a.py` runs new code

## Phase 5 — Charset
- [x] 12. Add Latin-1→UTF-8 transcoding in `world/world.go readLoop` when `Cfg.Charset == "latin-1"` (use `golang.org/x/text/encoding/charmap`) → Verify: latin-1 bytes decode correctly
- [x] 13. Add Telnet CHARSET option (byte 42) in `proto/proto.go` — accept server DO CHARSET, reply WILL, subneg UTF-8 → Verify: proto test with CHARSET negotiation sequence

## Phase 6 — Polish
- [x] 14. Truecolor RGB in `ansi/ansi.go` — store `FGRGB/BGRGB [3]uint8` in LineAttrs; render via `tcell.NewRGBColor()` in styles.go → Verify: `\e[38;2;255;0;0m` renders red in TUI
- [x] 15. Mouse scroll in `tui/tui.go` — handle `*tcell.EventMouse` WheelUp/WheelDown → `output.ScrollUp/Down(3)` → Verify: mouse wheel scrolls output pane
- [x] 16. Word-wrap in `tui/output.go` — break wrapped lines at word boundary using `uniseg` → Verify: long line wraps at space, not mid-word
- [x] 17. Tab completion in `tui/tui.go` — on ActionComplete: `/`-prefix → complete command names; else → complete macro/world names → Verify: `/co<Tab>` completes to `/connect`

## Done When
- [x] `go test ./...` green after every phase
- [x] No `panic` paths in production code paths
- [x] JS timers fire; `/repeat` works; `/ps` lists them
- [x] GMCP events reachable from JS and Python scripts
- [x] Latin-1 MUD text renders correctly
