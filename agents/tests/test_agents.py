import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from loupe_agents.agent import run_agent
from loupe_agents.fake import FakeBehavior, FakeProvider
from loupe_agents.ledger import BudgetExceeded, Prices, SpendLedger
from loupe_agents.providers import MissingAPIKey, OpenAICompatProvider, load_api_key
from loupe_agents.tools import Workspace
from loupe_sdk import InMemoryExporter, Tracer

REF = "def add(a, b):\n    return a + b\n"
STUB = "def add(a, b):\n    raise NotImplementedError\n"
VIS = ("import unittest\nfrom solution import *\n\nclass T(unittest.TestCase):\n"
       "    def test(self):\n        self.assertEqual(add(1, 2), 3)\n")


# --- ledger -----------------------------------------------------------------

def test_ledger_math_and_hard_stop(tmp_path):
    led = SpendLedger(str(tmp_path / "l.json"), cap=0.01)
    assert led.prices.cost(1_000_000, 1_000_000) == pytest.approx(0.8 + 2.7)
    led.check(1000, 1000)                       # worst case ¥0.0035 fits
    led.record(1000, 1000)
    led.record(1000, 1000)                      # spent ¥0.007
    with pytest.raises(BudgetExceeded):
        led.check(1000, 1000)                   # 0.007 + 0.0035 > 0.01
    assert led.spent == pytest.approx(0.007)


def test_ledger_persists_and_resumes_with_new_cap(tmp_path):
    path = str(tmp_path / "l.json")
    SpendLedger(path, cap=4.5).record(100_000, 10_000, {"model": "m"})
    again = SpendLedger(path, cap=0.05)
    assert again.state["calls"] == 1 and again.spent == pytest.approx(0.08 + 0.027)
    with pytest.raises(BudgetExceeded):        # lowered cap is enforced immediately
        again.check(1, 1)
    assert not [f for f in os.listdir(tmp_path) if f.startswith(".ledger-")]  # no temp files left


# --- api key handling -----------------------------------------------------

def test_key_sources(tmp_path, monkeypatch):
    monkeypatch.delenv("DASHSCOPE_API_KEY", raising=False)
    monkeypatch.delenv("LOUPE_KEYFILE", raising=False)
    with pytest.raises(MissingAPIKey):
        load_api_key()
    kf = tmp_path / "key"
    kf.write_text("sk-from-file\n")
    monkeypatch.setenv("LOUPE_KEYFILE", str(kf))
    assert load_api_key() == "sk-from-file"
    monkeypatch.setenv("DASHSCOPE_API_KEY", "sk-from-env")
    assert load_api_key() == "sk-from-env"


# --- OpenAI-compatible provider against a local mock server --------------

class Mock(BaseHTTPRequestHandler):
    requests = []
    plan = []  # list of (status, body)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        Mock.requests.append({"auth": self.headers.get("Authorization"), "body": body})
        status, payload = Mock.plan.pop(0) if Mock.plan else (200, completion("done"))
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(payload).encode())

    def log_message(self, *a):
        pass


def completion(text="", tool=None, pt=120, ct=30):
    msg = {"role": "assistant", "content": text}
    if tool:
        msg["tool_calls"] = [{"id": "c1", "type": "function", "function": {"name": tool[0], "arguments": json.dumps(tool[1])}}]
    return {"model": "qwen3.8-flash", "choices": [{"message": msg}], "usage": {"prompt_tokens": pt, "completion_tokens": ct}}


@pytest.fixture
def mock(monkeypatch):
    Mock.requests, Mock.plan = [], []
    srv = HTTPServer(("127.0.0.1", 0), Mock)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    monkeypatch.setenv("DASHSCOPE_API_KEY", "sk-test-123")
    yield f"http://127.0.0.1:{srv.server_port}"
    srv.shutdown()


def test_provider_parses_tool_calls_and_records_spend(mock, tmp_path):
    Mock.plan = [(429, {"error": "slow down"}), (200, completion(tool=("read_file", {"path": "a.py"})))]
    led = SpendLedger(str(tmp_path / "l.json"), cap=1.0)
    p = OpenAICompatProvider(base_url=mock, ledger=led, max_retries=2)
    comp = p.complete([{"role": "user", "content": "hi"}], tools=[{"type": "function"}])
    assert comp.tool_calls[0].name == "read_file" and comp.tool_calls[0].arguments == {"path": "a.py"}
    assert comp.input_tokens == 120 and led.state["calls"] == 1  # the 429 was retried, billed once
    sent = Mock.requests[-1]
    assert sent["auth"] == "Bearer sk-test-123" and sent["body"]["enable_thinking"] is False
    assert sent["body"]["model"] == "qwen3.8-flash"


def test_provider_refuses_call_over_budget(mock, tmp_path):
    led = SpendLedger(str(tmp_path / "l.json"), cap=0.000001)
    p = OpenAICompatProvider(base_url=mock, ledger=led)
    with pytest.raises(BudgetExceeded):
        p.complete([{"role": "user", "content": "x" * 1000}])
    assert Mock.requests == []  # nothing was sent


def test_provider_error_does_not_leak_key(mock):
    Mock.plan = [(400, {"error": "bad request"})]
    p = OpenAICompatProvider(base_url=mock, max_retries=0)
    with pytest.raises(RuntimeError) as e:
        p.complete([{"role": "user", "content": "x"}])
    assert "sk-test-123" not in str(e.value)


# --- workspace ------------------------------------------------------------

def test_workspace_sandbox_and_tests():
    ws = Workspace({"solution.py": STUB, "test_visible.py": VIS}, test_timeout=5)
    try:
        out, ok = ws.read_file("../../etc/passwd")
        assert not ok and "escapes" in out
        out, ok = ws.run_tests()
        assert not ok and "FAILED" in out
        ws.write_file("solution.py", REF)
        out, ok = ws.run_tests()
        assert ok and "OK" in out
        ws.write_file("solution.py", "import time\ntime.sleep(30)\n")
        out, ok = ws.run_tests(timeout=1)
        assert not ok and "timed out" in out
        assert ws.call("nope", {})[1] is False and ws.call("read_file", {})[1] is False
    finally:
        ws.close()


# --- agent loop with the fake provider ------------------------------------

def test_agent_loop_with_fake_rework_is_traced():
    mem = InMemoryExporter()
    tracer = Tracer("t", mem)
    fake = FakeProvider(REF, STUB, behavior=FakeBehavior(p_loop=1.0, p_rework=1.0, p_give_up=0.0, max_wrong=2))
    ws = Workspace({"solution.py": STUB, "test_visible.py": VIS})
    try:
        res = run_agent(fake, tracer, ws, "You are the CODER.", "Implement add.", max_steps=20)
        assert ws.run_tests()[1]
    finally:
        ws.close()
    assert res.final_text.startswith("DONE") and res.writes["solution.py"] >= 2
    kinds = [s.kind for s in mem.spans]
    assert kinds.count("agent") == 1 and kinds.count("llm") == res.llm_calls
    failed_tests = [s for s in mem.spans if s.kind == "tool" and s.status == "error"]
    assert failed_tests, "failing run_tests calls must be error spans"
    reads = [s for s in mem.spans if s.attributes.get("gen_ai.tool.name") == "read_file"]
    assert len(reads) >= 4  # the scripted loop re-read solution.py


def test_fake_is_deterministic():
    a = FakeProvider(REF, STUB, seed=1, task_id="t", arm="A").plan
    b = FakeProvider(REF, STUB, seed=1, task_id="t", arm="A").plan
    c = FakeProvider(REF, STUB, seed=1, task_id="t", arm="B").plan
    assert a == b and isinstance(c, list)
