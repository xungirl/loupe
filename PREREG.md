# Preregistration: does the harness matter for a small coding agent?

Status: **registered before any real-model run.** Written 2026-11-22. The
only runs so far used the deterministic fake provider (a pipeline dry run).
Anything changed after the first real run must be listed under "Deviations"
at the bottom with a date and reason.

## 1. Question

"Agent = Model + Harness." Holding the model fixed, do two cheap harness
changes improve a small coding agent?

* **Restate first** — before coding, the model restates the task (inputs,
  outputs, edge cases, plan) and the coder sees that restatement.
* **Multi-agent** — a planner writes a plan, a coder implements, a reviewer
  checks the code + visible test output and can send it back.

## 2. Hypotheses

* **H1 (restate):** arm B has a higher hidden-test pass rate than arm A.
* **H2 (multi-agent):** arm C has a higher hidden-test pass rate than arm A.
* **H3 (cost):** B costs little extra (paired token ratio B/A ≤ 1.3) and C costs
  substantially more (ratio C/A > 1.5).

Secondary, descriptive only (no claims): rework (extra `write_file` calls on
`solution.py`), steps, wall time, and Loupe detector findings (loop, rework)
per arm.

## 3. Design

* Within-task design: every task is run under every arm.
* **Model:** `qwen3.8-flash` via DashScope's OpenAI-compatible endpoint,
  `temperature=0`, `max_tokens=1500` per call, thinking disabled
  (`enable_thinking=false`).
* **Arms** (code: `experiment/loupe_exp/arms.py`):
  * **A** — single coder agent (tool loop, ≤12 model calls).
  * **B** — one restatement call (no tools), then the same coder with the
    restatement appended to its task message.
  * **C** — planner (1 call) → coder (≤12 calls) → reviewer (runs the visible
    tests, 1 call); on `REQUEST_CHANGES` the coder continues its conversation
    for ≤6 more calls; at most 2 review rounds.
* All arms share the same coder system prompt, tools (`list_files`,
  `read_file`, `write_file`, `run_tests` — visible tests only) and workspace.
* **Repetitions:** 3, run in repetition-major order (all of rep 0, then rep 1,
  then rep 2). Within a repetition, tasks run in order and the arm order is
  shuffled per task with seed 20261122, so no arm systematically runs first.

## 4. Tasks

24 small, self-written Python function/class tasks
(`experiment/tasks/t01…t24`, MIT-licensed with this repo): FizzBuzz,
palindromes, Roman numerals both ways, interval merging, two-sum, bracket
matching, word frequencies, flatten, chunk, run-length encode/decode, Caesar
cipher, anagram groups, first-occurrence binary search, transpose, spiral
order, longest common prefix, duration parsing, LRU cache, moving average,
dedupe, camelCase→snake_case, top-k frequent.

Each task has a prompt, a stub, a reference solution, 1 visible test case
(the agent can run it) and 4–7 hidden assertion statements, some looping over several inputs (only used for
scoring). `make experiment-verify` checks that every reference passes both
test files and every stub fails the hidden tests; this was verified on
2026-11-22 (24/24, `results/experiment/task_verification.json`).

Known limitation: these tasks are easy and well known. A ceiling effect
(every arm near 100%) is a plausible outcome and would itself be a finding:
"on easy tasks the harness does not matter, it only costs tokens".

## 5. Outcome measures

* **Primary:** hidden-test pass (boolean) per run, scored by running
  `test_hidden.py` against the final `solution.py` in a fresh directory with
  a 20 s timeout. A crashed run counts as recorded with its error and is
  excluded (reported separately).
* **Cost:** input + output tokens per run, as reported by the API `usage`
  field; cost in CNY at ¥0.8 / M input and ¥2.7 / M output tokens.
* **Secondary:** model calls, tool calls, rework, wall time, detector findings.

## 6. Analysis plan (code: `experiment/loupe_exp/analyze.py`)

* Unit of analysis: the **task** (n = 24). Per task and arm, the outcome is
  the pass rate over the completed repetitions.
* Only **complete repetitions** (all 24 × 3 cells present) enter the analysis;
  runs from a repetition cut short by the budget are reported but excluded.
* Per arm: mean pass rate with a 95 % percentile bootstrap CI over tasks
  (10,000 resamples, seed 12345).
* Primary comparisons **B−A** (H1) and **C−A** (H2): mean per-task difference
  with a paired 95 % bootstrap CI over tasks (same settings), plus
  wins/losses/ties. A hypothesis is **supported** only if the CI lies entirely
  above 0, **contradicted** if entirely below 0, otherwise **inconclusive**.
  No correction is applied for two primary comparisons; both are reported.
  If only one repetition completes, an exact McNemar test is also reported.
* H3: paired geometric-mean token ratio with a 95 % bootstrap CI.
* C−B is reported as secondary.
* Everything else is descriptive.

## 7. Budget and stopping rules

* Hard cap **¥4.50** for the whole experiment, enforced in code by
  `SpendLedger` before every model call: the call is refused if the money
  spent plus the call's worst case (estimated prompt tokens + 1500 output
  tokens) would exceed the cap.
* Planning figure (from the fake-provider dry run only, so a rough guess:
  the fake writes short scripted messages and its token counts are
  characters/4): about ¥0.41 per repetition of 72 runs, i.e. about ¥1.2 for
  3 repetitions. Real usage may be several times higher; that is why the
  analysis only uses complete repetitions and the cap is enforced per call.
* **Stop** when (a) all 3 repetitions are complete, or (b) the ledger refuses
  a call (budget stop), or (c) more than 20 % of runs in a repetition crash
  for provider/infrastructure reasons (then fix and restart that repetition
  from scratch, logged as a deviation).
* No peeking-based stopping: results are not analysed before the run ends,
  except for checking crash rates.

## 8. What would change my mind

If B or C shows no improvement with a CI that excludes even a small benefit
(e.g. upper bound < +0.05), I will conclude that on tasks this easy, extra
harness steps mostly cost tokens. If C helps but costs > 1.5× tokens, the
result is a cost/quality trade-off, not a free win.

## Deviations

None yet.
