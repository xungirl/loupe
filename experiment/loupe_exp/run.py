"""Run the experiment matrix (tasks × arms × repetitions), resumably.

    python -m loupe_exp.run --provider fake --out results/experiment/dryrun-fake
    python -m loupe_exp.run --provider dashscope --out results/experiment/real-YYYYMMDD

* Resumable: every finished run is appended to runs.jsonl; on restart, runs
  already in the file are skipped.
* Budget: real runs go through SpendLedger (ledger.json); when the next call
  could exceed the cap, the run in progress is abandoned (not recorded) and
  the process exits with code 3. Raise --cap or stop there.
* Tracing: every run is a trace (spans.jsonl, and live to Loupe if --loupe-url
  is reachable).
"""
from __future__ import annotations

import argparse
import json
import os
import random
import sys
import time
import urllib.request
from pathlib import Path

from loupe_agents.fake import FakeProvider
from loupe_agents.ledger import BudgetExceeded, Prices, SpendLedger
from loupe_agents.providers import DASHSCOPE_BASE_URL, DEFAULT_MODEL, OpenAICompatProvider, have_api_key
from loupe_agents.tools import Workspace
from loupe_sdk import FileExporter, MultiExporter, OTLPHttpExporter, Tracer

from .arms import run_arm
from .tasks import load_tasks, score

EXIT_BUDGET = 3
EXIT_NO_KEY = 2


def server_up(url: str) -> bool:
    try:
        with urllib.request.urlopen(url.rstrip("/") + "/healthz", timeout=1) as r:
            return r.status == 200
    except OSError:
        return False


def done_keys(path: Path) -> set:
    if not path.exists():
        return set()
    keys = set()
    for line in path.read_text().splitlines():
        try:
            keys.add(json.loads(line)["key"])
        except (json.JSONDecodeError, KeyError):
            continue  # a torn last line from a crash is ignored and re-run
    return keys


def schedule(task_ids: list, arms: list, reps: int, seed: int) -> list:
    """Task-major order with arms shuffled per task, so no arm systematically runs first/last."""
    rng = random.Random(seed)
    out = []
    for rep in range(reps):
        for t in task_ids:
            order = list(arms)
            rng.shuffle(order)
            out += [(t, a, rep) for a in order]
    return out


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--provider", choices=["fake", "dashscope"], required=True)
    ap.add_argument("--model", default=DEFAULT_MODEL)
    ap.add_argument("--base-url", default=DASHSCOPE_BASE_URL, help="any OpenAI-compatible endpoint")
    ap.add_argument("--out", required=True)
    ap.add_argument("--arms", default="A,B,C")
    ap.add_argument("--reps", type=int, default=1)
    ap.add_argument("--tasks", default="all", help="comma-separated task ids or 'all'")
    ap.add_argument("--seed", type=int, default=20261122)
    ap.add_argument("--cap", type=float, default=4.50, help="hard spend cap in CNY (real provider)")
    ap.add_argument("--loupe-url", default=os.environ.get("LOUPE_URL", "http://127.0.0.1:7070"))
    ap.add_argument("--limit", type=int, default=0, help="stop after N new runs (smoke tests)")
    args = ap.parse_args(argv)

    if args.provider == "dashscope" and not have_api_key():
        print("refusing to run: set DASHSCOPE_API_KEY or LOUPE_KEYFILE=/path/to/keyfile "
              "(or use --provider fake for a dry run)", file=sys.stderr)
        return EXIT_NO_KEY

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    tasks = {t.id: t for t in load_tasks()}
    task_ids = sorted(tasks) if args.tasks == "all" else args.tasks.split(",")
    arms = args.arms.split(",")
    prices = Prices()
    ledger = SpendLedger(str(out / "ledger.json"), cap=args.cap, prices=prices) if args.provider == "dashscope" else None

    meta = {"provider": args.provider, "model": args.model if args.provider == "dashscope" else "fake-scripted-1",
            "arms": arms, "tasks": task_ids, "reps": args.reps, "seed": args.seed, "cap_cny": args.cap,
            "is_dry_run": args.provider == "fake"}
    (out / "meta.json").write_text(json.dumps(meta, indent=2))

    exporters = [FileExporter(str(out / "spans.jsonl"))]
    if server_up(args.loupe_url):
        exporters.append(OTLPHttpExporter(args.loupe_url))
        print(f"streaming traces to {args.loupe_url}", file=sys.stderr)
    tracer = Tracer(f"experiment-{args.provider}", MultiExporter(*exporters))

    runs_path = out / "runs.jsonl"
    if runs_path.exists() and runs_path.stat().st_size and not runs_path.read_bytes().endswith(b"\n"):
        with open(runs_path, "a") as fh:  # a crash left a torn line: start the next record on a new line
            fh.write("\n")
    done = done_keys(runs_path)
    plan = schedule(task_ids, arms, args.reps, args.seed)
    todo = [p for p in plan if f"{p[0]}|{p[1]}|{p[2]}" not in done]
    print(f"{len(plan)} runs planned, {len(done)} already done, {len(todo)} to go", file=sys.stderr)

    new = 0
    try:
        for task_id, arm, rep in todo:
            task = tasks[task_id]
            if args.provider == "fake":
                provider = FakeProvider(task.reference, task.stub, seed=args.seed + rep, task_id=task_id, arm=arm)
            else:
                provider = OpenAICompatProvider(model=args.model, base_url=args.base_url, ledger=ledger)
            ws = Workspace({"solution.py": task.stub, "test_visible.py": task.visible_tests})
            started = time.monotonic()
            error = ""
            try:
                with tracer.agent(f"run {arm}:{task_id}", agent_id="run") as root:
                    root.set_attribute("experiment.arm", arm)
                    root.set_attribute("experiment.task", task_id)
                    result = run_arm(arm, task.prompt, provider, tracer, ws)
                solution, _ = ws.read_file("solution.py")
                visible_out, visible_ok = ws.run_tests()
            except BudgetExceeded:
                raise
            except Exception as e:  # a crashed run is recorded as a failure, not hidden
                error = f"{type(e).__name__}: {e}"
                result, solution, visible_ok = None, "", False
            finally:
                ws.close()
            passed, _ = score(task, solution) if solution else (False, "")
            in_tok = result.input_tokens if result else 0
            out_tok = result.output_tokens if result else 0
            rec = {"key": f"{task_id}|{arm}|{rep}", "task": task_id, "arm": arm, "rep": rep, "passed": passed,
                   "visible_passed": visible_ok, "input_tokens": in_tok, "output_tokens": out_tok,
                   "cost_cny": round(prices.cost(in_tok, out_tok), 6),
                   "llm_calls": result.llm_calls if result else 0, "tool_calls": result.tool_calls if result else 0,
                   "rework": max(0, (result.writes.get("solution.py", 0) - 1)) if result else 0,
                   "hit_max_steps": bool(result and result.hit_max_steps), "wall_s": round(time.monotonic() - started, 3),
                   "trace_id": root.trace_id, "error": error, "provider": meta["provider"], "model": meta["model"],
                   "finished_at": time.strftime("%Y-%m-%dT%H:%M:%S")}
            with open(runs_path, "a") as fh:
                fh.write(json.dumps(rec) + "\n")
            new += 1
            spent = f" spent ¥{ledger.spent:.4f}/{args.cap}" if ledger else ""
            print(f"[{len(done) + new}/{len(plan)}] {task_id} {arm} passed={passed} tokens={in_tok}+{out_tok}{spent}",
                  file=sys.stderr)
            if args.limit and new >= args.limit:
                break
    except BudgetExceeded as e:
        print(f"BUDGET STOP: {e}. {new} new runs recorded; re-run with a higher --cap to resume.", file=sys.stderr)
        tracer.shutdown()
        return EXIT_BUDGET
    except KeyboardInterrupt:
        print(f"interrupted; {new} new runs recorded. Re-run the same command to resume.", file=sys.stderr)
        tracer.shutdown()
        return 130
    tracer.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
