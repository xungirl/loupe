# Detector evaluation

Generated with (2026-11-22, Apple M2):

```bash
bin/loupe corpus -n 300 -seed 42              # → results/corpus/corpus.jsonl (deterministic, not committed)
bin/loupe eval-detectors -split train -out results/detectors/train.json
bin/loupe eval-detectors -split test  -out results/detectors/test.json
```

Protocol: thresholds in `server/internal/detect/detect.go:DefaultConfig` were
adjusted in two rounds while looking only at the train split (round 1: budget
token thresholds raised after clean traces exceeded them; loop detector given
the "no edit between repeats" rule after edit/test cycles were flagged as
loops; the generator was also changed so successive edits carry different
content). The test split was then evaluated once; `test.txt` is that result
(re-running reproduces it exactly — the corpus and detectors are deterministic).

Caveat: the corpus is synthetic and written by the same author as the
detectors. These numbers say the detectors catch the failure patterns *as
modelled*; they are not an estimate of accuracy on real agent traces.
