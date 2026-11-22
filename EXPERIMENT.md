# Running the experiment (morning checklist)

The experiment is fully implemented and was dry-run end to end with the fake
provider. Nothing has been sent to a real model yet. Read `PREREG.md` first —
it fixes the hypotheses and analysis before you see any data.

## 0. One-time setup (≈2 min)

```bash
cd ~/loupe
make build                      # Go binary
uv sync                         # Python env (stdlib-only runtime + pytest)
make experiment-verify          # 24/24 tasks: reference passes, stub fails
```

## 1. Give it the key (never commit it)

Either export it for this shell:

```bash
export DASHSCOPE_API_KEY=sk-...          # only in your terminal
```

or keep it in a file outside the repo and point to it:

```bash
export LOUPE_KEYFILE=~/.config/loupe/dashscope.key   # file contains just the key
```

The provider reads the key at call time from one of those two places only.
Without either, `make experiment` refuses to start (exit code 2).

## 2. (Optional) watch it live in Loupe

In a second terminal:

```bash
make demo        # or: bin/loupe serve -db data/experiment.db -web web/dist
```

If a server answers on `http://127.0.0.1:7070`, the runner streams every
agent run to it, so you can watch the Live page and open traces as they
finish. Not required — spans are always written to `spans.jsonl` too.

## 3. Run

```bash
make experiment                 # = 24 tasks × 3 arms × 3 repetitions, cap ¥4.50
```

What happens:

* Runs go to `results/experiment/real-qwen3.8-flash/` (`runs.jsonl`,
  `spans.jsonl`, `ledger.json`, `meta.json`).
* Before **every** model call, `SpendLedger` refuses the call if
  `spent + worst case of this call > ¥4.50` (worst case = estimated prompt +
  1500 output tokens at ¥0.8/M in, ¥2.7/M out). On refusal the runner prints
  `BUDGET STOP`, exits with code 3, and does not record the half-finished run.
* Each run prints `[k/216] task arm passed=… tokens=…  spent ¥x/4.5`.
* Planning figure from the dry run (fake provider, so only a rough guess):
  ~¥0.41 per repetition, ~¥1.2 for all three. Real usage may be several
  times higher; the cap holds regardless.

**Interrupted?** (Ctrl-C, laptop sleep, network error) — run `make experiment`
again. Finished runs are skipped; the ledger remembers what was spent.

Useful variants:

```bash
make experiment REPS=1                    # one repetition only (72 runs)
make experiment CAP=2.00                  # lower cap (the new cap applies on resume too)
PYTHONPATH=sdk/python:agents:experiment uv run python -m loupe_exp.run \
    --provider dashscope --out results/experiment/smoke --tasks t01_fizzbuzz --arms A --limit 1   # one-call smoke test
```

## 4. Report

```bash
make experiment-report          # RUN=real-qwen3.8-flash by default
```

This loads the run's spans into `results/experiment/<run>/loupe.db`, runs the
Loupe detectors (loop / rework / …) over every agent run, then writes
`report.json` and `report.md` with the preregistered analysis: pass rate per
arm with bootstrap CIs, paired B−A and C−A differences with CIs and
wins/losses/ties, token ratios, and cost. Only complete repetitions are
analysed (PREREG §6).

The **Experiments** page in the UI renders every
`results/experiment/*/report.json`; the dry run is shown with a "DRY RUN"
banner, a real run without it.

## 5. Before you write anything down

* Compare with the hypotheses in `PREREG.md` §2 and the decision rule in §6
  (supported / contradicted / inconclusive by whether the CI crosses 0).
* If anything deviated from the plan (crash restarts, a changed cap, a
  dropped task), add it under "Deviations" in `PREREG.md` with the date.
* Commit `results/experiment/real-qwen3.8-flash/` (except `loupe.db`, which is
  gitignored). It contains model outputs and token counts, no key.

## Files

| file | what |
|---|---|
| `experiment/tasks/t*.py` | 24 tasks: prompt, stub, reference, visible + hidden tests |
| `experiment/loupe_exp/arms.py` | arms A / B / C and their prompts |
| `experiment/loupe_exp/run.py` | resumable runner, budget stop, tracing |
| `experiment/loupe_exp/analyze.py` | preregistered analysis |
| `agents/loupe_agents/ledger.py` | the spend ledger (hard cap) |
| `agents/loupe_agents/providers.py` | DashScope / OpenAI-compatible provider |
| `agents/loupe_agents/fake.py` | deterministic fake provider used for the dry run |
| `results/experiment/dryrun-fake/` | the committed dry run (fake numbers, not results) |
