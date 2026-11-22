# Experiment report — dryrun-fake

> **DRY RUN — fake deterministic provider. These numbers exercise the pipeline and are NOT experimental results.**

Provider `fake`, model `fake-scripted-1`, 72/72 runs, spend ¥0.0000 of cap ¥4.5.

| arm | n | pass rate | 95% CI | mean tokens | mean ¥ | steps | rework |
|---|---|---|---|---|---|---|---|
| A single agent | 24 | 0.875 | [0.750, 1.000] | 6340 | 0.00535 | 7.5 | 1.12 |
| B single agent + restate task | 24 | 0.792 | [0.625, 0.958] | 6484 | 0.00555 | 8.3 | 0.83 |
| C planner/coder/reviewer | 24 | 0.833 | [0.667, 0.958] | 7143 | 0.00607 | 9.7 | 0.83 |

| comparison | Δ pass rate | 95% CI | W/L/T | McNemar p | token ratio |
|---|---|---|---|---|---|
| B-A (primary) | -0.083 | [-0.292, +0.125] | 3/5/16 | 0.727 | 1.02 |
| C-A (primary) | -0.042 | [-0.208, +0.125] | 2/3/19 | 1.000 | 1.08 |
| C-B | +0.042 | [-0.125, +0.208] | 3/2/19 | 1.000 | 1.05 |

Planning estimate: projected full-run cost ¥0.4075. Planning figure only: mean tokens per run from THIS run (fake provider: tokens = characters/4 of the scripted conversation; real usage will differ), priced at ¥0.8/M input and ¥2.7/M output.

- DRY RUN with the deterministic fake provider: pass rates and token counts are scripted and carry no information about real agents. They only prove the pipeline works end to end.
