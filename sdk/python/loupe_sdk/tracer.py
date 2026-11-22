"""Spans, the current-span context, and the Tracer API."""
from __future__ import annotations

import contextvars
import functools
import json
import secrets
import time
from typing import Any, Callable, Optional

# The span that new spans should use as parent. A ContextVar (not a global or
# thread-local) so it is correct across threads *and* asyncio tasks.
_current: contextvars.ContextVar[Optional["Span"]] = contextvars.ContextVar("loupe_current_span", default=None)

# Loupe span kind → OTel GenAI operation name.
_OPERATION = {"llm": "chat", "tool": "execute_tool", "agent": "invoke_agent"}

MAX_TEXT = 4000


def _clip(value: Any) -> str:
    text = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False, default=str)
    return text if len(text) <= MAX_TEXT else text[:MAX_TEXT] + "…"


class Span:
    """One unit of work. Use it through Tracer's context managers."""

    def __init__(self, tracer: "Tracer", name: str, kind: str, parent: Optional["Span"], attributes: dict):
        self.tracer = tracer
        self.name = name
        self.kind = kind
        self.trace_id = parent.trace_id if parent else secrets.token_hex(16)  # 32 hex chars, OTel format
        self.span_id = secrets.token_hex(8)                                  # 16 hex chars
        self.parent_id = parent.span_id if parent else ""
        # Agent identity is inherited so every span knows which agent it belongs to.
        self.agent_id = parent.agent_id if parent else ""
        self.agent_name = parent.agent_name if parent else ""
        self.start_ns = time.time_ns()
        self.end_ns = 0
        self.status = "ok"
        self.error = ""
        self.attributes: dict[str, Any] = {"loupe.kind": kind}
        if kind in _OPERATION:
            self.attributes["gen_ai.operation.name"] = _OPERATION[kind]
        self.attributes.update(attributes)

    # --- enrichment helpers -------------------------------------------------
    def set_attribute(self, key: str, value: Any) -> "Span":
        self.attributes[key] = value
        return self

    def set_usage(self, input_tokens: int = 0, output_tokens: int = 0, model: str | None = None) -> "Span":
        self.attributes["gen_ai.usage.input_tokens"] = int(input_tokens)
        self.attributes["gen_ai.usage.output_tokens"] = int(output_tokens)
        if model:
            self.attributes["gen_ai.response.model"] = model
        return self

    def set_result(self, result: Any) -> "Span":
        self.attributes["gen_ai.tool.call.result"] = _clip(result)
        return self

    def set_error(self, message: str, error_type: str = "error") -> "Span":
        self.status = "error"
        self.error = message[:MAX_TEXT]
        self.attributes["error.type"] = error_type
        return self

    def end(self) -> None:
        if self.end_ns == 0:
            self.end_ns = time.time_ns()
            self.tracer._export(self)

    # --- wire formats ------------------------------------------------------
    def to_otlp(self) -> dict:
        """OTLP/JSON span (ids hex, int64 as strings, typed attribute values)."""
        def any_value(v: Any) -> dict:
            if isinstance(v, bool):
                return {"boolValue": v}
            if isinstance(v, int):
                return {"intValue": str(v)}
            if isinstance(v, float):
                return {"doubleValue": v}
            return {"stringValue": v if isinstance(v, str) else json.dumps(v, default=str)}

        attrs = dict(self.attributes)
        if self.agent_id:
            attrs.setdefault("gen_ai.agent.id", self.agent_id)
        if self.agent_name:
            attrs.setdefault("gen_ai.agent.name", self.agent_name)
        span = {
            "traceId": self.trace_id,
            "spanId": self.span_id,
            "name": self.name,
            "startTimeUnixNano": str(self.start_ns),
            "endTimeUnixNano": str(self.end_ns or self.start_ns),
            "attributes": [{"key": k, "value": any_value(v)} for k, v in attrs.items()],
        }
        if self.parent_id:
            span["parentSpanId"] = self.parent_id
        if self.status == "error":
            span["status"] = {"code": 2, "message": self.error}
        return span

    def to_native(self, source: str) -> dict:
        """Loupe's native span JSON (what `loupe ingest-file` reads)."""
        a = self.attributes
        return {
            "trace_id": self.trace_id, "span_id": self.span_id, "parent_span_id": self.parent_id,
            "kind": self.kind, "name": self.name, "source": source,
            "agent_id": self.agent_id, "agent_name": self.agent_name,
            "start_ns": self.start_ns, "end_ns": self.end_ns or self.start_ns,
            "status": self.status, "error_msg": self.error,
            "model": a.get("gen_ai.response.model") or a.get("gen_ai.request.model", ""),
            "input_tokens": a.get("gen_ai.usage.input_tokens", 0),
            "output_tokens": a.get("gen_ai.usage.output_tokens", 0),
            "tool_name": a.get("gen_ai.tool.name", ""),
            "tool_input": a.get("gen_ai.tool.call.arguments", ""),
            "tool_output": a.get("gen_ai.tool.call.result", ""),
        }


class _SpanContext:
    """Context manager that makes a span current, ends it, and records exceptions."""

    def __init__(self, span: Span):
        self.span = span
        self.token: Optional[contextvars.Token] = None

    def __enter__(self) -> Span:
        self.token = _current.set(self.span)
        return self.span

    def __exit__(self, exc_type, exc, tb) -> bool:
        if exc is not None and self.span.status != "error":
            self.span.set_error(str(exc) or exc_type.__name__, exc_type.__name__)
        self.span.end()
        _current.reset(self.token)
        return False  # never swallow the exception


class Tracer:
    """Creates spans and hands finished ones to an exporter."""

    def __init__(self, service_name: str, exporter=None):
        self.service_name = service_name
        self.exporter = exporter

    @staticmethod
    def current() -> Optional[Span]:
        return _current.get()

    def span(self, name: str, kind: str = "step", **attributes) -> _SpanContext:
        return _SpanContext(Span(self, name, kind, _current.get(), attributes))

    def agent(self, name: str, agent_id: str | None = None, **attributes) -> _SpanContext:
        span = Span(self, f"invoke_agent {name}", "agent", _current.get(), attributes)
        span.agent_id = agent_id or f"{name}-{span.span_id[:6]}"
        span.agent_name = name
        return _SpanContext(span)

    def llm(self, model: str, **attributes) -> _SpanContext:
        return self.span(f"chat {model}", "llm", **{"gen_ai.request.model": model, **attributes})

    def tool(self, name: str, arguments: Any = None, **attributes) -> _SpanContext:
        attrs = {"gen_ai.tool.name": name, **attributes}
        if arguments is not None:
            attrs["gen_ai.tool.call.arguments"] = _clip(arguments)
        return self.span(f"execute_tool {name}", "tool", **attrs)

    def trace(self, kind: str = "step", name: str | None = None) -> Callable:
        """Decorator: run the function inside a span named after it."""
        def wrap(fn: Callable) -> Callable:
            span_name = name or fn.__qualname__

            @functools.wraps(fn)
            def inner(*args, **kwargs):
                if kind == "tool":
                    ctx = self.tool(span_name, arguments={"args": [repr(a) for a in args], "kwargs": {k: repr(v) for k, v in kwargs.items()}})
                else:
                    ctx = self.span(span_name, kind)
                with ctx as span:
                    result = fn(*args, **kwargs)
                    if kind == "tool":
                        span.set_result(result)
                    return result
            return inner
        return wrap

    def _export(self, span: Span) -> None:
        if self.exporter is not None:
            self.exporter.export(span, self.service_name)

    def flush(self, timeout: float = 5.0) -> None:
        if self.exporter is not None:
            self.exporter.flush(timeout)

    def shutdown(self, timeout: float = 5.0) -> None:
        if self.exporter is not None:
            self.exporter.shutdown(timeout)
