import asyncio
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from loupe_sdk import FileExporter, InMemoryExporter, OTLPHttpExporter, Tracer


def test_parenting_agent_identity_and_attributes():
    mem = InMemoryExporter()
    t = Tracer("svc", mem)
    with t.agent("planner") as agent:
        with t.llm("qwen3.8-flash") as llm:
            llm.set_usage(input_tokens=100, output_tokens=7)
        with t.tool("read_file", arguments={"path": "a.py"}) as tool:
            tool.set_result("print(1)")
    llm_span, tool_span, agent_span = mem.spans  # children end first
    assert llm_span.parent_id == agent.span_id and tool_span.parent_id == agent.span_id
    assert llm_span.trace_id == agent.trace_id == tool_span.trace_id and len(agent.trace_id) == 32
    assert llm_span.agent_name == "planner" and llm_span.agent_id == agent.agent_id
    assert llm_span.attributes["gen_ai.operation.name"] == "chat"
    assert llm_span.attributes["gen_ai.usage.input_tokens"] == 100
    assert tool_span.attributes["gen_ai.tool.call.arguments"] == '{"path": "a.py"}'
    assert agent_span.end_ns >= agent_span.start_ns > 0


def test_exception_marks_error_and_propagates():
    mem = InMemoryExporter()
    t = Tracer("svc", mem)
    with pytest.raises(ValueError):
        with t.tool("run_tests"):
            raise ValueError("boom")
    (span,) = mem.spans
    assert span.status == "error" and span.error == "boom" and span.attributes["error.type"] == "ValueError"
    assert Tracer.current() is None  # context restored


def test_decorator_and_asyncio_isolation():
    mem = InMemoryExporter()
    t = Tracer("svc", mem)

    @t.trace(kind="tool")
    def add(a, b):
        return a + b

    assert add(2, 3) == 5
    assert mem.spans[-1].attributes["gen_ai.tool.call.result"] == "5"

    async def worker(name):
        with t.agent(name) as a:
            await asyncio.sleep(0.01)
            with t.span("step") as s:
                return a.span_id, s.parent_id

    async def main():
        return await asyncio.gather(worker("x"), worker("y"))

    for agent_id, parent in asyncio.run(main()):
        assert agent_id == parent  # concurrent tasks don't see each other's spans


def test_otlp_shape():
    mem = InMemoryExporter()
    t = Tracer("svc", mem)
    with t.llm("m") as s:
        s.set_usage(5, 6)
        s.set_error("rate limited", "RateLimit")
    o = mem.spans[0].to_otlp()
    attrs = {a["key"]: a["value"] for a in o["attributes"]}
    assert attrs["gen_ai.usage.input_tokens"] == {"intValue": "5"}
    assert attrs["gen_ai.request.model"] == {"stringValue": "m"}
    assert o["status"] == {"code": 2, "message": "rate limited"}
    assert isinstance(o["startTimeUnixNano"], str)


def test_file_exporter_native_format(tmp_path):
    path = tmp_path / "spans.jsonl"
    t = Tracer("svc", FileExporter(str(path)))
    with t.agent("coder"):
        with t.tool("write_file", arguments={"path": "x.py"}):
            pass
    rows = [json.loads(l) for l in path.read_text().splitlines()]
    assert [r["kind"] for r in rows] == ["tool", "agent"]
    assert rows[0]["tool_name"] == "write_file" and rows[0]["agent_name"] == "coder" and rows[0]["source"] == "svc"


class _Server(BaseHTTPRequestHandler):
    received = []
    fail_first = 0

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if _Server.fail_first > 0:
            _Server.fail_first -= 1
            self.send_response(429)
            self.end_headers()
            return
        for rs in body["resourceSpans"]:
            for ss in rs["scopeSpans"]:
                _Server.received.extend(ss["spans"])
        self.send_response(202)
        self.end_headers()

    def log_message(self, *a):
        pass


@pytest.fixture
def server():
    _Server.received, _Server.fail_first = [], 0
    srv = HTTPServer(("127.0.0.1", 0), _Server)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    yield srv
    srv.shutdown()


def test_otlp_exporter_batches_and_retries_429(server):
    _Server.fail_first = 2  # server pushes back twice
    exp = OTLPHttpExporter(f"http://127.0.0.1:{server.server_port}", batch_size=50, interval=0.05)
    t = Tracer("svc", exp)
    for i in range(120):
        with t.span(f"s{i}"):
            pass
    t.shutdown()
    assert len(_Server.received) == 120 and exp.sent == 120 and exp.failed == 0


def test_otlp_exporter_drops_when_queue_full_and_never_blocks():
    # Nothing listens on port 9: every send fails; the queue fills; export() must not block.
    exp = OTLPHttpExporter("http://127.0.0.1:9", max_queue=5, batch_size=1, interval=0.01, timeout=0.05, max_retries=0)
    t = Tracer("svc", exp)
    for i in range(200):
        with t.span(f"s{i}"):
            pass
    t.shutdown(timeout=2)
    assert exp.dropped > 0 and exp.sent == 0
