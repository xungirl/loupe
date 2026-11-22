# Experiment report — real-qwen3.8-flash

Provider `dashscope`, model `qwen3.8-flash`, 216/216 runs, spend ¥1.4157 of cap ¥4.5.

| arm | n | pass rate | 95% CI | mean tokens | mean ¥ | steps | rework |
|---|---|---|---|---|---|---|---|
| A single agent | 72 | 0.986 | [0.958, 1.000] | 4620 | 0.00431 | 4.6 | 0.15 |
| B single agent + restate task | 72 | 1.000 | [1.000, 1.000] | 7523 | 0.00737 | 6.0 | 0.31 |
| C planner/coder/reviewer | 72 | 1.000 | [1.000, 1.000] | 8141 | 0.00798 | 7.1 | 0.18 |

| comparison | Δ pass rate | 95% CI | W/L/T | McNemar p | token ratio |
|---|---|---|---|---|---|
| B-A (primary) | +0.014 | [+0.000, +0.042] | 1/0/23 | – | 1.67 |
| C-A (primary) | +0.014 | [+0.000, +0.042] | 1/0/23 | – | 1.62 |
| C-B | +0.000 | [+0.000, +0.000] | 0/0/24 | – | 0.97 |

Planning estimate: projected full-run cost ¥1.4157. Planning figure only: mean tokens per run from THIS run, priced at ¥0.8/M input and ¥2.7/M output.

