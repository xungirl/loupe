"""Load the coding tasks from experiment/tasks/*.py and check them."""
from __future__ import annotations

import importlib.util
import os
from dataclasses import dataclass
from pathlib import Path

from loupe_agents.tools import Workspace

TASKS_DIR = Path(__file__).resolve().parents[1] / "tasks"


@dataclass(frozen=True)
class Task:
    id: str
    prompt: str
    stub: str
    reference: str
    visible_tests: str
    hidden_tests: str


def load_tasks(directory: Path = TASKS_DIR) -> list[Task]:
    tasks = []
    for path in sorted(directory.glob("t*.py")):
        spec = importlib.util.spec_from_file_location(path.stem, path)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        tasks.append(Task(id=path.stem, prompt=mod.PROMPT, stub=mod.STUB.lstrip("\n"),
                          reference=mod.REFERENCE.lstrip("\n"), visible_tests=mod.VISIBLE_TESTS.lstrip("\n"),
                          hidden_tests=mod.HIDDEN_TESTS.lstrip("\n")))
    return tasks


def score(task: Task, solution: str, timeout: float = 20.0) -> tuple[bool, str]:
    """Run the hidden tests against a solution in a fresh workspace."""
    ws = Workspace({"solution.py": solution, "test_hidden.py": task.hidden_tests})
    try:
        out, ok = ws.run_tests("test_hidden.py", timeout=timeout)
        return ok, out
    finally:
        ws.close()


def verify(tasks: list[Task]) -> list[dict]:
    """Every reference must pass visible+hidden tests; every stub must fail hidden tests."""
    rows = []
    for t in tasks:
        ref_hidden, _ = score(t, t.reference)
        ws = Workspace({"solution.py": t.reference, "test_visible.py": t.visible_tests})
        ref_visible, _ = ws.run_tests()
        ws.close()
        stub_hidden, _ = score(t, t.stub)
        rows.append({"task": t.id, "reference_passes_hidden": ref_hidden, "reference_passes_visible": ref_visible,
                     "stub_fails_hidden": not stub_hidden,
                     "ok": ref_hidden and ref_visible and not stub_hidden})
    return rows


if __name__ == "__main__":
    import json
    import sys
    rows = verify(load_tasks())
    bad = [r for r in rows if not r["ok"]]
    print(json.dumps({"tasks": len(rows), "all_ok": not bad, "failures": bad}, indent=2))
    sys.exit(1 if bad else 0)
