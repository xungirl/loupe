"""Analysis exactly as preregistered (PREREG.md §6).

Unit of analysis: the task. For each task and arm, the outcome is the hidden-
test pass rate over repetitions (0/1 with one repetition).
  * per arm: mean pass rate, 95% bootstrap CI over tasks (10,000 resamples)
  * paired comparisons B−A and C−A (primary), C−B (secondary): mean per-task
    difference with a paired bootstrap CI, wins/losses/ties, and an exact
    McNemar test on discordant tasks when there is one repetition
  * cost side: tokens per run, paired token ratio (geometric mean, bootstrap CI)

    python -m loupe_exp.analyze results/experiment/<run> [--db findings.db]
"""
from __future__ import annotations

import argparse
import json
import math
import random
import sqlite3
import statistics
import sys
import time
from collections import defaultdict
from pathlib import Path

from loupe_agents.ledger import Prices

from .arms import ARM_LABELS

N_BOOT = 10_000
BOOT_SEED = 12345


def bootstrap_ci(values: list[float], stat=statistics.fmean, n: int = N_BOOT, seed: int = BOOT_SEED) -> list[float]:
    if not values:
        return [float("nan"), float("nan")]
    rng = random.Random(seed)
    k = len(values)
    stats = sorted(stat([values[rng.randrange(k)] for _ in range(k)]) for _ in range(n))
    return [stats[int(0.025 * n)], stats[int(0.975 * n) - 1]]


def mcnemar_exact(b: int, c: int) -> float:
    """Two-sided exact McNemar p-value from discordant counts b and c."""
    n = b + c
    if n == 0:
        return 1.0
    tail = sum(math.comb(n, i) for i in range(min(b, c) + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def load_runs(run_dir: Path) -> list[dict]:
    runs = []
    for line in (run_dir / "runs.jsonl").read_text().splitlines():
        try:
            runs.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    return runs


def findings_by_trace(db: Path | None) -> dict:
    """{trace_id: {detector: count}} from a Loupe database, if one was built."""
    if not db or not db.exists():
        return {}
    out: dict = defaultdict(dict)
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    for trace_id, detector, n in con.execute("SELECT trace_id, detector, COUNT(*) FROM findings GROUP BY 1, 2"):
        out[trace_id][detector] = n
    con.close()
    return out


def analyze(run_dir: Path, db: Path | None = None) -> dict:
    meta = json.loads((run_dir / "meta.json").read_text())
    all_runs = load_runs(run_dir)
    arms = meta["arms"]
    # PREREG §6: the primary analysis uses complete repetitions only (every task × arm
    # present). Runs from a repetition cut short by the budget are reported, not analysed.
    per_rep = defaultdict(set)
    for r in all_runs:
        per_rep[r.get("rep", 0)].add((r["task"], r["arm"]))
    full = len(meta["tasks"]) * len(arms)
    complete_reps = sorted(rep for rep, cells in per_rep.items() if len(cells) == full)
    partial = [r for r in all_runs if r.get("rep", 0) not in complete_reps] if complete_reps else []
    used = [r for r in all_runs if r.get("rep", 0) in complete_reps] if complete_reps else all_runs
    runs = [r for r in used if not r.get("error")]
    errored = [r for r in used if r.get("error")]
    findings = findings_by_trace(db)
    prices = Prices()

    # per (task, arm): average over repetitions
    cell: dict = defaultdict(list)
    for r in runs:
        cell[(r["task"], r["arm"])].append(r)
    tasks = sorted({r["task"] for r in runs})
    complete_tasks = [t for t in tasks if all(cell.get((t, a)) for a in arms)]

    def per_task(arm: str, key) -> dict:
        return {t: statistics.fmean(key(r) for r in cell[(t, arm)]) for t in complete_tasks}

    summary = []
    for a in arms:
        rs = [r for r in runs if r["arm"] == a]
        if not rs:
            continue
        p = per_task(a, lambda r: 1.0 if r["passed"] else 0.0)
        summary.append({
            "arm": a, "label": ARM_LABELS.get(a, a), "n": len(rs),
            "pass_rate": statistics.fmean(p.values()) if p else None,
            "pass_ci95": bootstrap_ci(list(p.values())) if p else None,
            "mean_tokens": statistics.fmean(r["input_tokens"] + r["output_tokens"] for r in rs),
            "mean_input_tokens": statistics.fmean(r["input_tokens"] for r in rs),
            "mean_output_tokens": statistics.fmean(r["output_tokens"] for r in rs),
            "mean_cost_cny": statistics.fmean(prices.cost(r["input_tokens"], r["output_tokens"]) for r in rs),
            "mean_steps": statistics.fmean(r["llm_calls"] for r in rs),
            "mean_rework": statistics.fmean(r["rework"] for r in rs),
            "mean_wall_s": statistics.fmean(r["wall_s"] for r in rs),
            "loop_findings": sum(findings.get(r["trace_id"], {}).get("loop", 0) for r in rs) if findings else None,
            "rework_findings": sum(findings.get(r["trace_id"], {}).get("rework", 0) for r in rs) if findings else None,
        })

    paired = []
    for comp, primary in (("B-A", True), ("C-A", True), ("C-B", False)):
        x, y = comp.split("-")
        if x not in arms or y not in arms or not complete_tasks:
            continue
        px = per_task(x, lambda r: 1.0 if r["passed"] else 0.0)
        py = per_task(y, lambda r: 1.0 if r["passed"] else 0.0)
        diffs = [px[t] - py[t] for t in complete_tasks]
        wins, losses = sum(d > 0 for d in diffs), sum(d < 0 for d in diffs)
        entry = {"comparison": comp, "primary": primary, "metric": "pass_rate", "n_tasks": len(diffs),
                 "diff": statistics.fmean(diffs), "ci95": bootstrap_ci(diffs),
                 "wins": wins, "losses": losses, "ties": len(diffs) - wins - losses}
        if meta.get("reps", 1) == 1:
            entry["mcnemar_p"] = mcnemar_exact(wins, losses)
        tx = per_task(x, lambda r: r["input_tokens"] + r["output_tokens"])
        ty = per_task(y, lambda r: r["input_tokens"] + r["output_tokens"])
        logs = [math.log(tx[t] / ty[t]) for t in complete_tasks if tx[t] > 0 and ty[t] > 0]
        if logs:
            lo, hi = bootstrap_ci(logs)
            entry["token_ratio"] = math.exp(statistics.fmean(logs))
            entry["token_ratio_ci95"] = [math.exp(lo), math.exp(hi)]
        paired.append(entry)

    per_task_rows = []
    for t in tasks:
        row = {"task": t}
        for a in arms:
            rs = cell.get((t, a), [])
            if rs:
                row[a] = {"passed": all(r["passed"] for r in rs), "pass_rate": statistics.fmean(r["passed"] for r in rs),
                          "tokens": round(statistics.fmean(r["input_tokens"] + r["output_tokens"] for r in rs)),
                          "steps": round(statistics.fmean(r["llm_calls"] for r in rs), 1),
                          "rework": round(statistics.fmean(r["rework"] for r in rs), 1),
                          "trace_id": rs[0]["trace_id"]}
        per_task_rows.append(row)

    planned = len(meta["tasks"]) * len(arms) * meta.get("reps", 1)
    ledger_path = run_dir / "ledger.json"
    spent = json.loads(ledger_path.read_text())["spent"] if ledger_path.exists() else 0.0
    est = {a["arm"]: {"input": a["mean_input_tokens"], "output": a["mean_output_tokens"]} for a in summary}
    projected = sum(prices.cost(v["input"], v["output"]) for v in est.values()) * len(meta["tasks"]) * meta.get("reps", 1)

    notes = []
    if not complete_reps:
        notes.append("No repetition is complete yet; this is an interim look at partial data, not the preregistered analysis.")
    if partial:
        notes.append(f"{len(partial)} runs from an incomplete repetition are excluded from the analysis (PREREG §6).")
    if meta.get("is_dry_run"):
        notes.append("DRY RUN with the deterministic fake provider: pass rates and token counts are scripted and "
                     "carry no information about real agents. They only prove the pipeline works end to end.")
    if errored:
        notes.append(f"{len(errored)} runs crashed and are excluded (see runs.jsonl 'error').")
    if len(complete_tasks) < len(tasks):
        notes.append(f"{len(tasks) - len(complete_tasks)} tasks lack a run in every arm and are excluded from paired tests.")

    return {
        "run_id": run_dir.name, "provider": meta["provider"], "model": meta["model"],
        "is_dry_run": meta.get("is_dry_run", False), "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "prereg": "PREREG.md", "tasks": len(meta["tasks"]), "arms": arms,
        "arm_labels": {a: ARM_LABELS.get(a, a) for a in arms},
        "runs_completed": len(runs), "runs_errored": len(errored), "runs_planned": planned,
        "runs_recorded": len(all_runs), "complete_reps": complete_reps,
        "spend": {"currency": "CNY", "spent": spent, "cap": meta.get("cap_cny")},
        "summary_by_arm": summary, "paired": paired, "per_task": per_task_rows,
        "planning_estimate": {
            "label": ("Planning figure only: mean tokens per run from THIS run"
                      + (" (fake provider: tokens = characters/4 of the scripted conversation; real usage will differ)"
                         if meta.get("is_dry_run") else "") + ", priced at ¥0.8/M input and ¥2.7/M output."),
            "tokens_per_run_by_arm": est, "projected_cost_cny": projected},
        "notes": notes,
    }


def to_markdown(rep: dict) -> str:
    lines = [f"# Experiment report — {rep['run_id']}", ""]
    if rep["is_dry_run"]:
        lines += ["> **DRY RUN — fake deterministic provider. These numbers exercise the pipeline and are NOT "
                  "experimental results.**", ""]
    lines += [f"Provider `{rep['provider']}`, model `{rep['model']}`, {rep['runs_completed']}/{rep['runs_planned']} runs, "
              f"spend ¥{rep['spend']['spent']:.4f} of cap ¥{rep['spend']['cap']}.", "",
              "| arm | n | pass rate | 95% CI | mean tokens | mean ¥ | steps | rework |", "|---|---|---|---|---|---|---|---|"]
    for a in rep["summary_by_arm"]:
        ci = a["pass_ci95"] or [float("nan")] * 2
        lines.append(f"| {a['arm']} {a['label']} | {a['n']} | {a['pass_rate']:.3f} | [{ci[0]:.3f}, {ci[1]:.3f}] | "
                     f"{a['mean_tokens']:.0f} | {a['mean_cost_cny']:.5f} | {a['mean_steps']:.1f} | {a['mean_rework']:.2f} |")
    lines += ["", "| comparison | Δ pass rate | 95% CI | W/L/T | McNemar p | token ratio |", "|---|---|---|---|---|---|"]
    for p in rep["paired"]:
        tr = f"{p['token_ratio']:.2f}" if "token_ratio" in p else "–"
        mp = f"{p['mcnemar_p']:.3f}" if "mcnemar_p" in p else "–"
        lines.append(f"| {p['comparison']}{' (primary)' if p['primary'] else ''} | {p['diff']:+.3f} | "
                     f"[{p['ci95'][0]:+.3f}, {p['ci95'][1]:+.3f}] | {p['wins']}/{p['losses']}/{p['ties']} | {mp} | {tr} |")
    pe = rep["planning_estimate"]
    lines += ["", f"Planning estimate: projected full-run cost ¥{pe['projected_cost_cny']:.4f}. {pe['label']}", ""]
    lines += [f"- {n}" for n in rep["notes"]]
    return "\n".join(lines) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("run_dir")
    ap.add_argument("--db", default=None, help="Loupe DB with findings for these traces")
    args = ap.parse_args(argv)
    run_dir = Path(args.run_dir)
    rep = analyze(run_dir, Path(args.db) if args.db else None)
    (run_dir / "report.json").write_text(json.dumps(rep, indent=2))
    (run_dir / "report.md").write_text(to_markdown(rep))
    print(to_markdown(rep))
    return 0


if __name__ == "__main__":
    sys.exit(main())
