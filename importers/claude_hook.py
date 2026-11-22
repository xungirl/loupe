#!/usr/bin/env python3
"""Stream Claude Code hook events into Loupe, live.

Claude Code runs this script for each configured hook event and pipes a JSON
object to stdin (session_id, transcript_path, hook_event_name, tool_name,
tool_input, tool_response, tool_use_id, prompt, ...). We turn it into Loupe
spans and POST them to the server.

Design rules for a hook:
  * never break or slow down Claude Code: short timeout, always exit 0,
    never write to stdout (stdout can be interpreted as a hook decision);
  * stdlib only, so it runs with any python3;
  * span ids match the transcript importer ("root", "tool:<tool_use_id>"),
    so importing the same session later merges instead of duplicating.

Config (environment variables):
  LOUPE_URL           default http://127.0.0.1:7070
  LOUPE_HOOK_TIMEOUT  seconds, default 0.5
  LOUPE_HOOK_STATE    dir for PreToolUse start times, default $TMPDIR/loupe-hook
  LOUPE_HOOK_LOG      optional file to append errors to (debugging)
"""
import hashlib
import json
import os
import sys
import tempfile
import time
import urllib.request

MAX_PAYLOAD = 4000


def now_ns() -> int:
    return time.time_ns()


def clip(value, limit=MAX_PAYLOAD) -> str:
    if value is None:
        return ""
    text = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False, default=str)
    return text if len(text) <= limit else text[:limit] + "…"


def state_dir(session_id: str) -> str:
    base = os.environ.get("LOUPE_HOOK_STATE") or os.path.join(tempfile.gettempdir(), "loupe-hook")
    safe = "".join(c for c in session_id if c.isalnum() or c in "-_") or "unknown"
    path = os.path.join(base, safe)
    os.makedirs(path, exist_ok=True)
    return path


def tool_key(event: dict) -> str:
    """Stable key for a tool call: its id if Claude Code sends one, else a hash."""
    if event.get("tool_use_id"):
        return str(event["tool_use_id"])
    raw = json.dumps([event.get("tool_name"), event.get("tool_input")], sort_keys=True, default=str)
    return "h" + hashlib.sha1(raw.encode()).hexdigest()[:16]


def target_of(tool_input) -> str:
    if isinstance(tool_input, dict):
        for key in ("file_path", "path", "notebook_path", "command", "url", "pattern"):
            if isinstance(tool_input.get(key), str) and tool_input[key]:
                return " ".join(tool_input[key].split())[:200]
    return ""


def is_error_response(event: dict) -> bool:
    if event.get("hook_event_name") == "PostToolUseFailure":
        return True
    resp = event.get("tool_response")
    if isinstance(resp, dict):
        if resp.get("is_error") is True or resp.get("success") is False:
            return True
        if resp.get("error"):
            return True
    return False


def build_spans(event: dict, ts: int) -> list:
    """Map one hook event to zero or more Loupe spans (pure function, tested)."""
    session = event.get("session_id") or "unknown"
    name = event.get("hook_event_name") or "unknown"
    trace = "cc-" + session
    common = {"trace_id": trace, "source": "claude-code", "agent_id": "main", "agent_name": "claude-code"}
    # The root span is re-sent with every event; the server ignores duplicates,
    # so the trace has a root even if the hook was installed mid-session.
    root = dict(common, span_id="root", kind="agent", name="claude-code session (live)",
                start_ns=ts, end_ns=ts,
                attributes=json.dumps({"cwd": event.get("cwd", ""), "transcript_path": event.get("transcript_path", "")}))
    spans = [root]

    if name == "PreToolUse":
        # Remember when the call started; the span is emitted on PostToolUse.
        path = os.path.join(state_dir(session), tool_key(event) + ".start")
        with open(path, "w") as fh:
            fh.write(str(ts))
        return spans

    if name in ("PostToolUse", "PostToolUseFailure"):
        key = tool_key(event)
        start = ts
        path = os.path.join(state_dir(session), key + ".start")
        try:
            with open(path) as fh:
                start = int(fh.read().strip() or ts)
            os.remove(path)
        except (OSError, ValueError):
            pass
        tool_input = event.get("tool_input")
        span = dict(common, span_id="tool:" + key if event.get("tool_use_id") else f"hook-tool:{key}:{ts}",
                    parent_span_id="root", kind="tool", name=event.get("tool_name") or "tool",
                    tool_name=event.get("tool_name") or "", tool_input=clip(tool_input),
                    tool_output=clip(event.get("tool_response") or event.get("error")), target=target_of(tool_input),
                    start_ns=min(start, ts), end_ns=ts)
        if is_error_response(event):
            span["status"] = "error"
            span["error_msg"] = clip(event.get("error") or event.get("tool_response"), 300)
        spans.append(span)
        return spans

    label = name
    attrs = {}
    if name == "UserPromptSubmit":
        prompt = event.get("prompt") or ""
        label = "user: " + " ".join(prompt.split())[:60]
        attrs["text"] = clip(prompt, 2000)
    elif name == "Notification":
        attrs["message"] = clip(event.get("message"), 500)
    elif name in ("SessionStart", "SessionEnd"):
        attrs["detail"] = event.get("source") or event.get("reason") or ""
    spans.append(dict(common, span_id=f"hook:{name}:{ts}", parent_span_id="root", kind="event",
                      name=label if name == "UserPromptSubmit" else f"hook: {name}",
                      start_ns=ts, end_ns=ts, attributes=json.dumps(attrs)))
    return spans


def send(spans: list) -> None:
    url = os.environ.get("LOUPE_URL", "http://127.0.0.1:7070").rstrip("/") + "/api/v1/spans"
    timeout = float(os.environ.get("LOUPE_HOOK_TIMEOUT", "0.5"))
    body = json.dumps({"spans": spans}).encode()
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        resp.read()


def main() -> int:
    try:
        event = json.load(sys.stdin)
        if isinstance(event, dict):
            send(build_spans(event, now_ns()))
    except Exception as exc:  # a hook must never fail the session
        log = os.environ.get("LOUPE_HOOK_LOG")
        if log:
            try:
                with open(log, "a") as fh:
                    fh.write(f"{time.strftime('%Y-%m-%dT%H:%M:%S')} {type(exc).__name__}: {exc}\n")
            except OSError:
                pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
