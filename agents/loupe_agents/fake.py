"""A deterministic, scripted stand-in for an LLM.

It speaks the same Completion interface as the real provider, so the agent,
the tracing, the ledger hooks, the experiment runner and the analysis all run
for real — only the "thinking" is scripted. Behaviour is drawn from a seeded
RNG per (seed, task, arm), so a dry run is reproducible.

Configurable failure behaviours (probabilities in FakeBehavior):
  * loop      — re-reads the same file several times before acting
  * rework    — writes 1-3 wrong versions (the stub) before the right one
  * give_up   — never finds the fix: keeps writing wrong versions, then stops

The fake knows the task's reference solution (that is how it "solves" tasks),
which is exactly why its pass rates mean nothing: dry-run numbers exercise the
pipeline and must never be read as results.
"""
from __future__ import annotations

import hashlib
import json
import random
import re
from dataclasses import dataclass

from .providers import Completion, ToolCall

FAKE_MODEL = "fake-scripted-1"


@dataclass
class FakeBehavior:
    p_loop: float = 0.15
    p_rework: float = 0.35
    p_give_up: float = 0.12
    max_wrong: int = 3


def _tokens(obj) -> int:
    """Rough token count for usage reporting (chars / 4)."""
    return max(1, len(obj if isinstance(obj, str) else json.dumps(obj, ensure_ascii=False)) // 4)


def _role(messages: list) -> str:
    m = re.search(r"You are the (\w+)", messages[0].get("content", "")) if messages else None
    return m.group(1).lower() if m else "coder"


class FakeProvider:
    def __init__(self, reference: str, stub: str, seed: int = 0, task_id: str = "", arm: str = "",
                 behavior: FakeBehavior | None = None):
        self.model = FAKE_MODEL
        self.reference = reference
        self.stub = stub
        self.behavior = behavior or FakeBehavior()
        h = int(hashlib.sha256(f"{seed}|{task_id}|{arm}".encode()).hexdigest()[:12], 16)
        self.rng = random.Random(h)
        self.calls = 0
        self.plan = self._make_plan()
        self.step = 0

    def _make_plan(self) -> list:
        """The coder's scripted actions for this run."""
        b, r = self.behavior, self.rng
        actions = [("read_file", {"path": "solution.py"})]
        if r.random() < b.p_loop:
            actions += [("read_file", {"path": "solution.py"})] * r.randint(2, 4)  # looping
        actions.append(("read_file", {"path": "test_visible.py"}))
        self.gives_up = r.random() < b.p_give_up
        wrong = b.max_wrong if self.gives_up else (r.randint(1, b.max_wrong) if r.random() < b.p_rework else 0)
        for i in range(wrong):
            actions += [("write_file", {"path": "solution.py", "content": f"# attempt {i + 1}\n" + self.stub}),
                        ("run_tests", {})]
        if not self.gives_up:
            actions += [("write_file", {"path": "solution.py", "content": self.reference}), ("run_tests", {})]
        return actions

    def complete(self, messages: list, tools: list | None = None) -> Completion:
        self.calls += 1
        role = _role(messages)
        if role == "restater":
            content = ("Restated task: implement the function exactly as specified. Inputs and outputs as described; "
                       "edge cases: empty input, boundaries, invalid values. Plan: read solution.py, implement, run tests.")
            return self._text(messages, content)
        if role == "planner":
            content = "Plan:\n1. Read solution.py and test_visible.py.\n2. Implement the function.\n3. Run the tests and fix failures."
            return self._text(messages, content)
        if role == "reviewer":
            last = messages[-1].get("content", "")
            last = last.split("TEST OUTPUT:", 1)[-1]  # judge the test run, not the code
            ok = "FAILED" not in last and "Error" not in last and re.search(r"\bOK\b", last)
            return self._text(messages, "APPROVE" if ok else "REQUEST_CHANGES: the visible tests still fail; fix solution.py.")
        return self._coder(messages, tools)

    def _coder(self, messages: list, tools) -> Completion:
        last = messages[-1]
        # React to a passing test run: done.
        if last.get("role") == "tool" and re.search(r"\bOK\b", last.get("content", "")) and "FAILED" not in last.get("content", ""):
            return self._text(messages, "DONE: all visible tests pass.", tools)
        if self.step < len(self.plan):
            name, args = self.plan[self.step]
            self.step += 1
            return self._tool(messages, name, args, tools)
        if self.gives_up:
            return self._text(messages, "I could not make the tests pass.", tools)
        # Re-invoked after review feedback: write the fix again and test.
        self.plan += [("write_file", {"path": "solution.py", "content": self.reference}), ("run_tests", {})]
        return self._coder(messages, tools)

    def _usage(self, messages, tools, out) -> tuple[int, int]:
        return _tokens(messages) + (_tokens(tools) if tools else 0), _tokens(out) + 5

    def _text(self, messages, content, tools=None) -> Completion:
        i, o = self._usage(messages, tools, content)
        return Completion(content=content, tool_calls=[], input_tokens=i, output_tokens=o, model=self.model,
                          message={"role": "assistant", "content": content})

    def _tool(self, messages, name, args, tools) -> Completion:
        call_id = f"call_{self.calls}"
        raw = json.dumps(args)
        i, o = self._usage(messages, tools, raw)
        msg = {"role": "assistant", "content": "",
               "tool_calls": [{"id": call_id, "type": "function", "function": {"name": name, "arguments": raw}}]}
        return Completion(content="", tool_calls=[ToolCall(call_id, name, args)], input_tokens=i, output_tokens=o,
                          model=self.model, message=msg)
