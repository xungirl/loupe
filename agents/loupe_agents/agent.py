"""The mini coding agent: a plain while-loop over model calls and tool calls.

    messages = [system, user]
    repeat up to max_steps:
        reply = model(messages, tools)          # one LLM span
        if reply has no tool calls: finished
        for each tool call: run it              # one tool span each
            append the result as a "tool" message

Every step is traced with loupe_sdk, so a run shows up in Loupe as an agent
span containing its LLM and tool spans.
"""
from __future__ import annotations

from dataclasses import dataclass, field

from loupe_sdk import Tracer

from .tools import TOOL_SCHEMAS, Workspace


@dataclass
class AgentResult:
    final_text: str = ""
    llm_calls: int = 0
    tool_calls: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    writes: dict = field(default_factory=dict)  # path → number of write_file calls
    hit_max_steps: bool = False
    messages: list = field(default_factory=list)

    def add_usage(self, comp) -> None:
        self.llm_calls += 1
        self.input_tokens += comp.input_tokens
        self.output_tokens += comp.output_tokens


def llm_call(provider, tracer: Tracer, messages: list, tools=None):
    """One traced model call."""
    with tracer.llm(provider.model) as span:
        comp = provider.complete(messages, tools)
        span.set_usage(comp.input_tokens, comp.output_tokens, comp.model)
        return comp


def chat(provider, tracer: Tracer, system: str, user: str, result: AgentResult | None = None) -> str:
    """A single tool-less exchange (planner, restater, reviewer)."""
    comp = llm_call(provider, tracer, [{"role": "system", "content": system}, {"role": "user", "content": user}])
    if result is not None:
        result.add_usage(comp)
    return comp.content


def run_loop(provider, tracer: Tracer, ws: Workspace, messages: list, result: AgentResult, max_steps: int = 12) -> AgentResult:
    """Continue the tool loop on an existing conversation (so a reviewer can send it back)."""
    result.messages = messages
    for _ in range(max_steps):
        comp = llm_call(provider, tracer, messages, TOOL_SCHEMAS)
        result.add_usage(comp)
        messages.append(comp.message)
        if not comp.tool_calls:
            result.final_text = comp.content
            return result
        for tc in comp.tool_calls:
            with tracer.tool(tc.name, arguments=tc.arguments) as span:
                output, ok = ws.call(tc.name, tc.arguments)
                span.set_result(output)
                if not ok:
                    span.set_error(output.splitlines()[-1] if output else "tool failed", "ToolError")
            result.tool_calls += 1
            if tc.name == "write_file":
                path = str(tc.arguments.get("path", ""))
                result.writes[path] = result.writes.get(path, 0) + 1
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": output})
    result.hit_max_steps = True
    return result


def run_agent(provider, tracer: Tracer, ws: Workspace, system: str, user: str, name: str = "coder",
              max_steps: int = 12) -> AgentResult:
    """Start a fresh coder conversation inside its own agent span."""
    with tracer.agent(name):
        msgs = [{"role": "system", "content": system}, {"role": "user", "content": user}]
        return run_loop(provider, tracer, ws, msgs, AgentResult(), max_steps)
