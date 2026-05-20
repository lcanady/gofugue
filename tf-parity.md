# TinyFugue Parity Plan

## Goal
Close the feature gap between gofugue and TinyFugue so the client is usable as a daily driver on any MUD that a TF user would run.

---

## Phase 1 — Output Control ✅

- [x] **`/gag`** — suppress lines matching a pattern. `TypeGag` added to macro.go; pipeline sets `ev.Gagged = true` on match; TUI skips gagged lines.
- [x] **`/hilite`** — highlight lines. `TypeHilite` added; on match `parseAttrSpec(body)` overrides `wle.Attrs` before TUI sees it.
- [x] **`/substitute`** — rewrite line text. `TypeSubstitute` added; on match `wle.Text` is replaced with capture-expanded body.
- [x] **Capture groups in trigger bodies** — `%0`–`%9` are replaced before `execBody`. Captures returned from `macro.Engine.FireTriggers` via `TriggerFire.Captures`.

---

## Phase 2 — Expression Engine ✅

- [x] **`expr.Eval()`** — recursive-descent evaluator: arithmetic, comparisons, boolean, string concat (`.`). Implemented in `internal/expr/eval.go`.
- [x] **Built-in functions (tier 1)** — `strlen`, `substr`, `tolower`, `toupper`, `trim`, `strcmp`, `strcat`, `replace`, `pad`, `time`, `ftime`, `rand`, `abs`, `sqrt`, `floor`, `ceil`, `min`, `max`, `if` (lazy), `regmatch` (sets scope vars), `ascii`, `char`.
- [x] **`{expr}` in ExpandFull** — `ExpandFull()` now finds `{...}` blocks and evaluates them via `Eval()`.
- [x] **`-E<expr>` condition gate** — compat sets `Enabled=false` for `-E`; engine skips disabled macros.

---

## Phase 3 — Trigger/Macro Richness ✅

- [x] **Pattern match modes (`-m` flag)** — `MatchMode` field on `Macro`; `MatchGlob` (translates `*`/`?`), `MatchSubstr` (literal), `MatchRegexp` (default). Parsed in both `compat.go` and `cmd.go`.
- [x] **One-shot triggers (`-n` shots)** — `Shots int` on `Macro`; engine decrements after each fire; auto-removes when exhausted (deadlock-safe post-lock removal).
- [x] **Fallthrough (`-F` flag)** — `Fallthru bool` on `Macro`; engine continues checking lower-priority triggers when set.
- [x] **Probability (`-c` flag)** — `Prob int` (0–100); `rand.Intn(100)` rolled; 0 = always fire.
- [x] **Invisible macros (`-i` flag)** — `Invisible bool`; `List()` omits invisible macros.

---

## Phase 4 — Extended Hook System ✅

- [x] **PROMPT hook** — partial-line buffer with 200ms timer in `readLoop`; publishes `HookEvent{Name:"PROMPT"}` when prompt detected.
- [x] **SEND hook** — published before alias check/world.Send in `userInputSub` goroutine.
- [x] **ACTIVITY hook** — published on every `WorldLineEvent` from a non-foreground world (in `readLoop`).
- [x] **RESIZE hook** — published in `tui.go` `handleTcellEvent` on `*tcell.EventResize`.

---

## Phase 5 — World Management Commands ✅

- [x] **`/addworld`** — `cmdAddWorld` parses `name url [char [pass]]`, calls `worldMgr.Add()`.
- [x] **`/listworlds`** — `cmdListWorlds` calls `worldMgr.WorldInfos()`, prints connected state.
- [x] **`/saveworld [file]`** — `cmdSaveWorld` calls `worldMgr.SaveTOML(path)` (default `worlds.toml`).
- [x] **`/unworld <name>`** — `cmdUnworld` calls `worldMgr.Remove(name)`.

---

## Phase 6 — Status Bar & Display ✅

- [x] **`/set` variable listing** — `cmdSet` with no args delegates to `cmdListVar`; `scope.All()` added.
- [x] **`/listvar [pattern]`** — registered as builtin; filters by pattern substring.
- [x] **`/restrict`** — `cmdRestrict` sets `restrictShell/File/World` flags in main.go.

---

## Phase 7 — TinyFugue Compat Completeness ✅

- [x] **`/set` loading in compat.go** — now populates `res.Settings`; `loadScript()` applies them to scope.
- [x] **`/key` bindings in compat.go** — now populates `res.KeyBindings`; stored for caller to apply.
- [x] **`/gag`, `/hilite` in compat.go** — `%gag`/`/gag` and `%hilite`/`/hilite` directives handled.
- [x] **Compat flags `-c`, `-n`, `-F`, `-i`, `-m`, `-h`** — all parsed in `parseDef()`.

---

## Phase 8 — Speedwalk & Quality of Life ✅

- [x] **Speedwalk expansion** — `expandSpeedwalk()` in main.go converts `3n2ew` → `north;north;north;east;east;west`.
- [x] **Multi-command body** — `execBody` splits on `;` and sends each part.
- [x] **`--connect` flag** — `--connect mud://host:port` connects on startup.
- [x] **`--script` flag** — `--script file.tf` loads a script on startup.

---

## Architecture Changes

- **Two-phase event flow**: `EvWorldLine` (raw from connection) → pipeline processes gag/hilite/substitute → `EvWorldLineRendered` → TUI. TUI now subscribes to `EvWorldLineRendered`.
- **`bus.WorldRenderedEvent`** wraps `WorldLineEvent` with new event type.
- **`world.Manager.readLoop`** now byte-based with prompt detection timer.
- **`macro.Engine.FireTriggers`** replaces `MatchTrigger` for full multi-trigger support; `MatchTrigger` is a backwards-compatible shim.

---

## Phase 9 — Final Gaps ✅

- [x] **`history.Search` regexp** — `Buffer.Search()` now compiles pattern as regexp; falls back to literal substring if pattern is invalid.
- [x] **`-E{expr}` condition gate at fire time** — `Macro.Condition string` added; `Engine.SetEvalFunc()` registers the evaluator; `FireTriggers` evaluates condition before each fire.
- [x] **Auto-login on CONNECT hook** — pipeline hook handler looks up `WorldConfig.Login` (or `Char`/`Pass`) and sends on CONNECT.
- [x] **Configurable status bar** — `StatusBar` refactored to `[]statusField` with `AddField`/`RemoveField`; default segments: app, world, conn, lag, time.
- [x] **`[MORE]` paging indicator** — `StatusBar.SetMore(bool)` called from `draw()` when `!output.AtBottom()`.
- [x] **`/restrict` enforcement** — `cmd.Context.IsRestricted` added; `/connect`, `/addworld`, `/load`, `/log`, `/save` check it before executing.

---

## Done When

- [x] Can load a real `.tf` script from a popular MUD without warnings for common directives
- [x] `/gag`, `/hilite`, `/substitute` work in interactive session
- [x] `{expr}` evaluation works for basic arithmetic and string functions
- [x] One-shot and fallthrough triggers work
- [x] Capture groups `%1`–`%9` expand in trigger bodies
- [x] World management commands (`/addworld`, `/saveworld`, `/listworlds`) round-trip correctly
- [x] `/recall` uses regexp search
- [x] `-E{expr}` evaluated at trigger fire time, not just define time
- [x] Auto-login fires on CONNECT for worlds with Char/Pass/Login config
- [x] Status bar shows `[MORE]` when scrolled up; fields are pluggable
- [x] `/restrict FILE/WORLD` blocks relevant commands
