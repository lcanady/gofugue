#!/usr/bin/env python3
"""GoFugue Python scripting bridge.

Launched by GoFugue as:
    python3 -u bridge.py <script.py>

Communicates with the GoFugue host process via newline-delimited JSON on
stdin (host → script) and stdout (script → host).  Stderr is left open for
debugging; GoFugue does not read it.

Usage in a script:
    import gofugue as tf

    @tf.on("CONNECT")
    def on_connect(world):
        tf.send(world, "look")

    @tf.def_trigger(r"You gain (\\d+) xp", name="xp_trigger")
    def on_xp(world, line, caps):
        tf.echo(f"XP gained: {caps[1]}")
"""

from __future__ import annotations

import importlib.util
import json
import sys
import threading
from typing import Callable

# ---------------------------------------------------------------------------
# Internal state
# ---------------------------------------------------------------------------

_send_lock = threading.Lock()
_hook_callbacks: dict[str, list[Callable]] = {}
_trigger_callbacks: dict[str, Callable] = {}  # name → fn
_gmcp_callbacks: dict[str, list[Callable]] = {}  # module (or "") → [fn]


def _write(msg: dict) -> None:
    with _send_lock:
        sys.stdout.write(json.dumps(msg) + "\n")
        sys.stdout.flush()


# ---------------------------------------------------------------------------
# Public API  (exposed as the `tf` / `gofugue` module)
# ---------------------------------------------------------------------------

def send(world: str, text: str) -> None:
    """Send text to a world."""
    _write({"t": "cmd", "method": "send", "world": world, "text": text})


def echo(text: str) -> None:
    """Print text to the GoFugue local output pane."""
    _write({"t": "cmd", "method": "echo", "text": str(text)})


log = echo  # alias


def getvar(name: str) -> str | None:
    """Read a GoFugue scope variable (synchronous via request/reply)."""
    # Simple blocking implementation: send request, read the next reply on a
    # dedicated pipe.  For simplicity we do not multiplex — scripts should
    # treat getvar as blocking.
    _write({"t": "cmd", "method": "getvar", "name": name})
    # The reader loop below will set _reply_event when the response arrives.
    _reply_event.clear()
    _reply_event.wait(timeout=2.0)
    return _reply_store.get(name)


def setvar(name: str, value: str) -> None:
    """Write a GoFugue scope variable."""
    _write({"t": "cmd", "method": "setvar", "name": name, "value": str(value)})


def on(hook_name: str) -> Callable:
    """Decorator: register a function as a hook callback.

    @tf.on("CONNECT")
    def handle_connect(world): ...
    """
    def decorator(fn: Callable) -> Callable:
        _hook_callbacks.setdefault(hook_name.upper(), []).append(fn)
        return fn
    return decorator


def def_trigger(pattern: str, *, name: str | None = None) -> Callable:
    """Decorator: register a trigger pattern.

    @tf.def_trigger(r"You gain (\\d+) xp", name="xp")
    def on_xp(world, line, caps): ...
    """
    def decorator(fn: Callable) -> Callable:
        tname = name if name else fn.__name__
        _trigger_callbacks[tname] = fn
        _write({"t": "cmd", "method": "def", "name": tname, "pattern": pattern})
        return fn
    return decorator


def undef(name: str) -> None:
    """Remove a trigger by name."""
    _trigger_callbacks.pop(name, None)
    _write({"t": "cmd", "method": "undef", "name": name})


def on_gmcp(module: str = "") -> Callable:
    """Decorator: register a GMCP callback.

    @tf.on_gmcp("Char.Vitals")
    def vitals(world, module, data): ...

    @tf.on_gmcp()   # wildcard — receives all GMCP
    def all_gmcp(world, module, data): ...
    """
    def decorator(fn: Callable) -> Callable:
        _gmcp_callbacks.setdefault(module, []).append(fn)
        return fn
    return decorator


# ---------------------------------------------------------------------------
# Internal reply storage for getvar
# ---------------------------------------------------------------------------

_reply_event = threading.Event()
_reply_store: dict[str, str] = {}


# ---------------------------------------------------------------------------
# Event reader loop (runs in main thread after script is loaded)
# ---------------------------------------------------------------------------

def _dispatch(msg: dict) -> None:
    t = msg.get("t")
    if t == "event":
        name = msg.get("name", "")
        if name == "HOOK":
            hook = msg.get("hook", "").upper()
            world = msg.get("world", "")
            for fn in _hook_callbacks.get(hook, []):
                try:
                    fn(world)
                except Exception as exc:  # noqa: BLE001
                    echo(f"[py] hook error ({hook}): {exc}")
        elif name == "TRIGGER":
            world = msg.get("world", "")
            line = msg.get("line", "")
            caps = msg.get("caps", [])
            trigger_name = msg.get("trigger", "")
            # Call the specific trigger callback if registered, else all.
            if trigger_name and trigger_name in _trigger_callbacks:
                try:
                    _trigger_callbacks[trigger_name](world, line, caps)
                except Exception as exc:  # noqa: BLE001
                    echo(f"[py] trigger error ({trigger_name}): {exc}")
            elif not trigger_name:
                for fn in list(_trigger_callbacks.values()):
                    try:
                        fn(world, line, caps)
                    except Exception as exc:  # noqa: BLE001
                        echo(f"[py] trigger error: {exc}")
        elif name == "GMCP":
            world = msg.get("world", "")
            module = msg.get("module", "")
            data = msg.get("data")
            for key in (module, ""):
                for fn in list(_gmcp_callbacks.get(key, [])):
                    try:
                        fn(world, module, data)
                    except Exception as exc:  # noqa: BLE001
                        echo(f"[py] GMCP error ({module}): {exc}")
    elif t == "reply":
        name = msg.get("name", "")
        _reply_store[name] = msg.get("value", "")
        _reply_event.set()
    elif t == "load":
        _load_file(msg.get("path", ""))


def _load_file(path: str) -> None:
    spec = importlib.util.spec_from_file_location("_gf_script", path)
    if spec is None:
        echo(f"[py] cannot load {path}")
        return
    mod = importlib.util.module_from_spec(spec)
    # Expose the bridge as both `tf` and `gofugue` inside the loaded script.
    import types
    api = types.ModuleType("gofugue")
    for name in ("send", "echo", "log", "getvar", "setvar", "on",
                 "def_trigger", "undef", "on_gmcp"):
        setattr(api, name, globals()[name])
    sys.modules["gofugue"] = api
    sys.modules["tf"] = api
    try:
        spec.loader.exec_module(mod)
    except Exception as exc:  # noqa: BLE001
        echo(f"[py] error loading {path}: {exc}")


def _reader_loop() -> None:
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw:
            continue
        try:
            msg = json.loads(raw)
            _dispatch(msg)
        except json.JSONDecodeError as exc:
            echo(f"[py] bad json: {exc}")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    script_path = sys.argv[1] if len(sys.argv) > 1 else None
    if script_path:
        _load_file(script_path)
    _reader_loop()
