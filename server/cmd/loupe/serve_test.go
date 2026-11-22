package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "loupe-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "loupe")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// TestGracefulShutdownFlushesQueue: spans accepted with 202 but not yet
// written (long flush interval) must be in SQLite after SIGTERM, and an open
// SSE stream must not block shutdown.
func TestGracefulShutdownFlushesQueue(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "g.db")
	addr := freePort(t)
	cmd := exec.Command(binPath, "serve", "-addr", addr, "-db", db, "-flush", "30s", "-batch", "1000000")
	var logs bytes.Buffer
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		if resp, err := http.Get(base + "/healthz"); err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Keep a live-tail (SSE) client connected: shutdown must not wait on it.
	sse, err := http.Get(base + "/api/v1/live")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()

	now := time.Now().UnixNano()
	accepted := 0
	for r := 0; r < 10; r++ {
		spans := make([]model.Span, 300)
		for i := range spans {
			spans[i] = model.Span{TraceID: fmt.Sprintf("t%d", r), SpanID: fmt.Sprint(i), Kind: "tool", StartNS: now, EndNS: now}
		}
		b, _ := json.Marshal(map[string]any{"spans": spans})
		resp, err := http.Post(base+"/api/v1/spans", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status %d", resp.StatusCode)
		}
		accepted += len(spans)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exit: %v\n%s", err, logs.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("shutdown hung:\n%s", logs.String())
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, n, _ := st.Counts(context.Background())
	if int(n) != accepted {
		t.Fatalf("after shutdown %d spans in DB, %d were accepted\n%s", n, accepted, logs.String())
	}
}
