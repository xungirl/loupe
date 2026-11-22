package detect

import (
	"sort"
	"strings"

	"github.com/xungirl/loupe/server/internal/corpus"
)

// Score is the confusion matrix of one detector at the trace level: a trace
// counts as predicted-positive if the detector emits at least one finding.
type Score struct {
	Detector  string  `json:"detector"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	// Error analysis: which generator variants were missed, and which true
	// labels the false positives carried ("clean" = no label).
	MissedVariants map[string]int `json:"missed_variants,omitempty"`
	FPFromLabels   map[string]int `json:"fp_from_labels,omitempty"`
}

// Report is the result of one evaluation run.
type Report struct {
	Split   string  `json:"split"`
	Traces  int     `json:"traces"`
	Clean   int     `json:"clean"`
	Scores  []Score `json:"scores"`
	MacroF1 float64 `json:"macro_f1"`
}

// Evaluate runs the detectors on every trace of the chosen split.
func Evaluate(traces []corpus.Trace, split string, cfg Config) Report {
	rep := Report{Split: split}
	scores := map[string]*Score{}
	for _, d := range All {
		scores[d] = &Score{Detector: d, MissedVariants: map[string]int{}, FPFromLabels: map[string]int{}}
	}
	for i := range traces {
		t := &traces[i]
		if split != "all" && t.Split != split {
			continue
		}
		rep.Traces++
		if len(t.Labels) == 0 {
			rep.Clean++
		}
		predicted := map[string]bool{}
		for _, f := range Run(t.Spans, cfg) {
			predicted[f.Detector] = true
		}
		for _, d := range All {
			s := scores[d]
			truth := t.HasLabel(d)
			switch {
			case truth && predicted[d]:
				s.TP++
			case truth && !predicted[d]:
				s.FN++
				for _, v := range strings.Split(t.Variant, ",") {
					if strings.HasPrefix(v, d+":") {
						s.MissedVariants[v]++
					}
				}
			case !truth && predicted[d]:
				s.FP++
				if len(t.Labels) == 0 {
					s.FPFromLabels["clean"]++
				}
				for _, l := range t.Labels {
					s.FPFromLabels[l]++
				}
			default:
				s.TN++
			}
		}
	}
	for _, d := range All {
		s := scores[d]
		s.Precision = ratio(s.TP, s.TP+s.FP)
		s.Recall = ratio(s.TP, s.TP+s.FN)
		if s.Precision+s.Recall > 0 {
			s.F1 = 2 * s.Precision * s.Recall / (s.Precision + s.Recall)
		}
		rep.Scores = append(rep.Scores, *s)
		rep.MacroF1 += s.F1 / float64(len(All))
	}
	sort.SliceStable(rep.Scores, func(i, j int) bool { return indexOf(rep.Scores[i].Detector) < indexOf(rep.Scores[j].Detector) })
	return rep
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func indexOf(d string) int {
	for i, x := range All {
		if x == d {
			return i
		}
	}
	return len(All)
}
