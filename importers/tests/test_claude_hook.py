"""Tests for the Claude Code hook adapter: pure mapping + the real script end to end."""
import json
import os
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import pytest

import claude_hook

SCRIPT = Path(__file__).resolve().parents[1] / "claude_hook.py"


@pytest.fixture(autouse=True)
def state_dir(tmp_path, monkeypatch):
    monkeypatch.setenv("LOUPE_HOOK_STATE", str(tmp_path / "state"))
    return tmp_path / "state"


def by_kind(spans, kind):
    return [s for s in spans if s["kind"] == kind]


def test_pre_then_post_tool_use_produces_one_timed_tool_span():
    base = {"session_id": "s1", "transcript_path": "/x.jsonl", "cwd": "/repo", "tool_name": "Bash",
            "tool_input": {"command": "pytest -q"}, "tool_use_id": "toolu_9"}
    pre = claude_hook.build_spans(dict(base, hook_event_name="PreToolUse"), 1_000)
    assert [s["span_id"] for s in pre] == ["root"]  # nothing but the root until the call ends
    post = claude_hook.build_spans(dict(base, hook_event_name="PostToolUse",
                                        tool_response={"stdout": "3 passed", "is_error": False}), 5_000)
    (tool,) = by_kind(post, "tool")
    assert tool["span_id"] == "tool:toolu_9"  # same id as the transcript importer
    assert tool["trace_id"] == "cc-s1" and tool["parent_span_id"] == "root"
    assert (tool["start_ns"], tool["end_ns"]) == (1_000, 5_000)
    assert tool["target"] == "pytest -q" and "status" not in tool


def test_failed_tool_is_an_error_span():
    ev = {"session_id": "s1", "hook_event_name": "PostToolUse", "tool_name": "Edit",
          "tool_input": {"file_path": "/a.py"}, "tool_response": {"error": "old_string not found"}}
    (tool,) = by_kind(claude_hook.build_spans(ev, 10), "tool")
    assert tool["status"] == "error" and tool["target"] == "/a.py"
    assert tool["span_id"].startswith("hook-tool:")  # no tool_use_id → unique fallback id


def test_prompt_and_lifecycle_events():
    spans = claude_hook.build_spans({"session_id": "s", "hook_event_name": "UserPromptSubmit",
                                     "prompt": "fix   the\nbug"}, 7)
    (ev,) = by_kind(spans, "event")
    assert ev["name"] == "user: fix the bug"
    assert json.loads(ev["attributes"])["text"] == "fix   the\nbug"
    (stop,) = by_kind(claude_hook.build_spans({"session_id": "s", "hook_event_name": "Stop"}, 8), "event")
    assert stop["name"] == "hook: Stop"


def test_large_payloads_are_clipped():
    ev = {"session_id": "s", "hook_event_name": "PostToolUse", "tool_name": "Read",
          "tool_input": {"file_path": "/big"}, "tool_response": "x" * 50_000}
    (tool,) = by_kind(claude_hook.build_spans(ev, 1), "tool")
    assert len(tool["tool_output"]) <= claude_hook.MAX_PAYLOAD + 1


class _Capture(BaseHTTPRequestHandler):
    bodies = []

    def do_POST(self):
        n = int(self.headers["Content-Length"])
        _Capture.bodies.append(json.loads(self.rfile.read(n)))
        self.send_response(202)
        self.end_headers()
        self.wfile.write(b"{}")

    def log_message(self, *args):
        pass


def run_script(stdin: str, env: dict) -> subprocess.CompletedProcess:
    return subprocess.run([sys.executable, str(SCRIPT)], input=stdin, capture_output=True, text=True,
                          env={**os.environ, **env}, timeout=10)


def test_script_posts_to_server_and_prints_nothing():
    server = HTTPServer(("127.0.0.1", 0), _Capture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        url = f"http://127.0.0.1:{server.server_port}"
        ev = {"session_id": "abc", "hook_event_name": "Notification", "message": "needs approval"}
        res = run_script(json.dumps(ev), {"LOUPE_URL": url})
        assert res.returncode == 0 and res.stdout == ""
        assert _Capture.bodies and _Capture.bodies[-1]["spans"][1]["name"] == "hook: Notification"
    finally:
        server.shutdown()


def test_script_never_fails_claude_code(tmp_path):
    log = tmp_path / "hook.log"
    started = time.monotonic()
    # Nothing listens on this port; also send garbage stdin.
    res = run_script("{not json", {"LOUPE_URL": "http://127.0.0.1:9", "LOUPE_HOOK_LOG": str(log)})
    assert res.returncode == 0 and res.stdout == ""
    res = run_script(json.dumps({"session_id": "s", "hook_event_name": "Stop"}),
                     {"LOUPE_URL": "http://127.0.0.1:9", "LOUPE_HOOK_LOG": str(log)})
    assert res.returncode == 0
    assert time.monotonic() - started < 8
    assert "JSONDecodeError" in log.read_text()
