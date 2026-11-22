// Command loupe is the single binary for the server and all offline tools.
//
//	loupe serve            run the HTTP server (ingest, query API, live tail, UI)
//	loupe import-claude    import Claude Code transcripts into a database
//	loupe ingest-file      load a spans JSONL file into a database
//	loupe corpus           generate the labelled fault-injection corpus
//	loupe eval-detectors   score detectors (precision / recall / F1) on the corpus
//	loupe loadgen          ingest load generator (benchmarks)
//	loupe bench-query      query/assembly latency benchmark on a large database
//	loupe gen-transcripts  synthetic Claude Code transcripts (importer benchmark)
//	loupe demo-seed        build the demo database
package main

import (
	"fmt"
	"os"
)

type command struct {
	name, help string
	run        func(args []string) error
}

func commands() []command {
	return []command{
		{"serve", "run the HTTP server", runServe},
		{"import-claude", "import Claude Code JSONL transcripts", runImportClaude},
		{"ingest-file", "load a spans JSONL file into a database", runIngestFile},
		{"corpus", "generate the labelled fault-injection corpus", runCorpus},
		{"eval-detectors", "precision/recall/F1 of detectors on the corpus", runEvalDetectors},
		{"detect", "run detectors over every trace in a database", runDetectAll},
		{"loadgen", "ingest load generator", runLoadgen},
		{"bench-query", "query and assembly latency benchmark", runBenchQuery},
		{"gen-transcripts", "write synthetic Claude Code transcripts", runGenTranscripts},
		{"demo-seed", "build the demo database", runDemoSeed},
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	for _, c := range commands() {
		if c.name == os.Args[1] {
			if err := c.run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "loupe "+c.name+":", err)
				os.Exit(1)
			}
			return
		}
	}
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loupe <command> [flags]\n\ncommands:")
	for _, c := range commands() {
		fmt.Fprintf(os.Stderr, "  %-16s %s\n", c.name, c.help)
	}
}
