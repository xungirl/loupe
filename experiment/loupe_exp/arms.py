"""The three experimental arms (see PREREG.md).

A  single agent:       coder loop only
B  restate first:      one model call restates the task (inputs, outputs, edge
                       cases, plan); the restatement is given to the coder
C  multi-agent:        planner → coder → reviewer (up to 2 review rounds; on
                       REQUEST_CHANGES the coder continues its conversation)

All arms share the same coder prompt, tools and step budget, so the only
difference is the harness around the coder.
"""
from __future__ import annotations

from loupe_agents.agent import AgentResult, chat, run_agent, run_loop
from loupe_agents.tools import Workspace
from loupe_sdk import Tracer

CODER_MAX_STEPS = 12
REVIEW_ROUNDS = 2
FOLLOWUP_STEPS = 6

CODER_SYSTEM = ("You are the CODER. You complete small Python tasks in a workspace. Tools: list_files, read_file, "
                "write_file, run_tests. solution.py contains a stub: replace it with a complete, correct implementation "
                "(write the whole file). Run the tests. When they pass, reply with a one-line summary and no tool call.")
RESTATER_SYSTEM = ("You are the RESTATER. Before any code is written, restate the task in your own words: the inputs, "
                   "the expected outputs, the edge cases, and a 3-step plan. Do not write code. Be brief.")
PLANNER_SYSTEM = ("You are the PLANNER. Write a short numbered implementation plan for the task, listing the edge "
                  "cases to handle. Do not write code. Be brief.")
REVIEWER_SYSTEM = ("You are the REVIEWER. You receive a task, the current solution.py and the output of the visible "
                   "tests. Reply with exactly APPROVE if the solution is correct and complete; otherwise reply "
                   "REQUEST_CHANGES: followed by the specific problems.")

ARM_LABELS = {"A": "single agent", "B": "single agent + restate task", "C": "planner/coder/reviewer"}


def task_message(prompt: str) -> str:
    return (f"Task:\n{prompt}\n\nWorkspace files: solution.py (stub to replace), test_visible.py (a few example "
            f"tests; hidden tests will also be run).")


def run_arm(arm: str, prompt: str, provider, tracer: Tracer, ws: Workspace) -> AgentResult:
    if arm == "A":
        return run_agent(provider, tracer, ws, CODER_SYSTEM, task_message(prompt), "coder", CODER_MAX_STEPS)

    if arm == "B":
        with tracer.agent("coder") as agent:
            total = AgentResult()
            with tracer.span("restate task", "step"):
                restated = chat(provider, tracer, RESTATER_SYSTEM, task_message(prompt), total)
            msgs = [{"role": "system", "content": CODER_SYSTEM},
                    {"role": "user", "content": task_message(prompt) + "\n\nYour restatement of the task:\n" + restated}]
            agent.set_attribute("loupe.restated", True)
            return run_loop(provider, tracer, ws, msgs, total, CODER_MAX_STEPS)

    if arm == "C":
        total = AgentResult()
        with tracer.agent("orchestrator"):
            with tracer.agent("planner"):
                plan = chat(provider, tracer, PLANNER_SYSTEM, task_message(prompt), total)
            coder_msgs = [{"role": "system", "content": CODER_SYSTEM},
                          {"role": "user", "content": task_message(prompt) + "\n\nPlan from the planner:\n" + plan}]
            with tracer.agent("coder", agent_id="coder") as coder:
                run_loop(provider, tracer, ws, coder_msgs, total, CODER_MAX_STEPS)
            for _ in range(REVIEW_ROUNDS):
                with tracer.agent("reviewer", agent_id="reviewer"):
                    with tracer.tool("run_tests") as span:
                        test_out, ok = ws.run_tests()
                        span.set_result(test_out)
                        if not ok:
                            span.set_error("visible tests fail", "TestFailure")
                    code, _ = ws.read_file("solution.py")
                    verdict = chat(provider, tracer, REVIEWER_SYSTEM,
                                   f"{task_message(prompt)}\n\nsolution.py:\n{code}\n\nTEST OUTPUT:\n{test_out}", total)
                if verdict.strip().upper().startswith("APPROVE"):
                    break
                coder_msgs.append({"role": "user", "content": "Reviewer feedback:\n" + verdict})
                with tracer.agent("coder", agent_id=coder.agent_id):
                    run_loop(provider, tracer, ws, coder_msgs, total, FOLLOWUP_STEPS)
        return total

    raise ValueError(f"unknown arm {arm}")
