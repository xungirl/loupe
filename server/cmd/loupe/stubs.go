package main

import (
	"context"
	"errors"

	"github.com/xungirl/loupe/server/internal/model"
)

var errTODO = errors.New("not implemented yet")

func runLoadgen(args []string) error        { return errTODO }
func runBenchQuery(args []string) error     { return errTODO }
func runGenTranscripts(args []string) error { return errTODO }
func runDemoSeed(args []string) error       { return errTODO }

func runDemoLive(ctx context.Context, submit func([]model.Span) error) {}
