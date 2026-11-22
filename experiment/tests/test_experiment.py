import json
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import pytest

from loupe_exp.analyze import analyze, bootstrap_ci, mcnemar_exact
from loupe_exp.run import main as run_main
from loupe_exp.run import schedule
from loupe_exp.tasks import load_tasks, verify

ROOT = Path(__file__).resolve().parents[2]


def test_tasks_are_valid():
    tasks = load_tasks()
    assert len(tasks) >= 20
    rows = verify(tasks)
    assert all(r["ok"] for r in rows), [r for r in rows if not r["ok"]]


def test_schedule_balanced_and_deterministic():
    s1 = schedule(["t1", "t2", "t3"], ["A", "B", "C"], 2, seed=1)
    assert s1 == schedule(["t1", "t2", "t3"], ["A", "B", "C"], 2, seed=1)
    assert len(s1) == 18 and len(set(s1)) == 18


def test_stats_helpers():
    assert mcnemar_exact(0, 0) == 1.0
    assert mcnemar_exact(0, 6) == pytest.approx(2 / 64)
    lo, hi = bootstrap_ci([1.0] * 10)
    assert lo == hi == 1.0
    lo, hi = bootstrap_ci([0.0, 1.0] * 20)
    assert 0.3 < lo < 0.5 < hi < 0.7


def test_dry_run_resume_and_analysis(tmp_path):
    out = tmp_path / "run"
    args = ["--provider", "fake", "--out", str(out), "--tasks", "t01_fizzbuzz,t02_palindrome", "--loupe-url", "http://127.0.0.1:1"]
    assert run_main(args + ["--limit", "2"]) == 0
    assert len((out / "runs.jsonl").read_text().splitlines()) == 2
    # simulate a crash mid-write: a torn last line must be ignored and that run redone
    with open(out / "runs.jsonl", "a") as fh:
        fh.write('{"key": "t02_palin')
    assert run_main(args) == 0
    keys = []
    for line in (out / "runs.jsonl").read_text().splitlines():
        try:
            keys.append(json.loads(line)["key"])
        except json.JSONDecodeError:
            assert line.startswith('{"key": "t02_palin')  # only the torn line is unreadable
    assert len(keys) == 6 and len(set(keys)) == 6
    rep = analyze(out)
    assert rep["is_dry_run"] and rep["runs_completed"] == 6 and len(rep["paired"]) == 3
    assert {a["arm"] for a in rep["summary_by_arm"]} == {"A", "B", "C"}
    assert (out / "spans.jsonl").exists()


def test_refuses_without_key(tmp_path):
    env = {"PATH": "/usr/bin:/bin", "PYTHONPATH": ":".join(str(ROOT / p) for p in ["sdk/python", "agents", "experiment"])}
    res = subprocess.run([sys.executable, "-m", "loupe_exp.run", "--provider", "dashscope", "--out", str(tmp_path / "x")],
                         capture_output=True, text=True, env=env)
    assert res.returncode == 2 and "refusing" in res.stderr
    assert not (tmp_path / "x").exists()


class Expensive(BaseHTTPRequestHandler):
    """An OpenAI-compatible mock whose every answer is a costly tool call."""
    def do_POST(self):
        self.rfile.read(int(self.headers["Content-Length"]))
        n = int(self.headers["Content-Length"])
        body = {"model": "qwen3.8-flash", "usage": {"prompt_tokens": n // 4, "completion_tokens": 100},
                "choices": [{"message": {"role": "assistant", "content": "", "tool_calls": [
                    {"id": "c", "type": "function", "function": {"name": "list_files", "arguments": "{}"}}]}}]}
        self.send_response(200)
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def log_message(self, *a):
        pass


def test_budget_stop_never_exceeds_cap(tmp_path, monkeypatch):
    srv = HTTPServer(("127.0.0.1", 0), Expensive)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    monkeypatch.setenv("DASHSCOPE_API_KEY", "sk-test")
    out = tmp_path / "real"
    try:
        code = run_main(["--provider", "dashscope", "--base-url", f"http://127.0.0.1:{srv.server_port}", "--out", str(out),
                         "--tasks", "t01_fizzbuzz", "--arms", "A", "--cap", "0.01", "--loupe-url", "http://127.0.0.1:1"])
    finally:
        srv.shutdown()
    ledger = json.loads((out / "ledger.json").read_text())
    assert code == 3
    assert 0 < ledger["spent"] <= 0.01 and ledger["calls"] >= 2
    assert not (out / "runs.jsonl").exists()  # the interrupted run is not recorded as a result
