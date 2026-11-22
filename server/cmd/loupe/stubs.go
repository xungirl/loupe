package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

var errTODO = errors.New("not implemented yet")

func runImportClaude(args []string) error   { return errTODO }
func runIngestFile(args []string) error     { return errTODO }
func runCorpus(args []string) error         { return errTODO }
func runEvalDetectors(args []string) error  { return errTODO }
func runDetectAll(args []string) error      { return errTODO }
func runLoadgen(args []string) error        { return errTODO }
func runBenchQuery(args []string) error     { return errTODO }
func runGenTranscripts(args []string) error { return errTODO }
func runDemoSeed(args []string) error       { return errTODO }

type traceDetector struct{}

func newTraceDetector(_ *store.Store) *traceDetector { return &traceDetector{} }
func (d *traceDetector) run(ctx context.Context, t store.TraceSummary) error { return nil }
func (d *traceDetector) httpHandler(r *http.Request, id string) ([]store.Finding, error) {
	return nil, errTODO
}
func runDemoLive(ctx context.Context, submit func([]model.Span) error) {}
