"""loupe_sdk — tiny tracing SDK for agent code (stdlib only).

    from loupe_sdk import Tracer, OTLPHttpExporter

    tracer = Tracer("my-agent", OTLPHttpExporter("http://127.0.0.1:7070"))
    with tracer.agent("coder"):
        with tracer.llm("qwen3.8-flash") as span:
            ...                                   # call the model
            span.set_usage(input_tokens=812, output_tokens=95)
        with tracer.tool("run_tests", arguments={"path": "."}) as span:
            span.set_result("3 passed")
    tracer.shutdown()                             # flush before exit

Attribute names follow the OpenTelemetry GenAI semantic conventions
(gen_ai.operation.name, gen_ai.request.model, gen_ai.usage.input_tokens, ...),
and OTLPHttpExporter speaks OTLP/HTTP JSON, so the same spans can go to any
OpenTelemetry backend, not just Loupe.
"""
from .exporters import FileExporter, InMemoryExporter, MultiExporter, OTLPHttpExporter
from .tracer import Span, Tracer

__all__ = ["Tracer", "Span", "OTLPHttpExporter", "FileExporter", "InMemoryExporter", "MultiExporter"]
