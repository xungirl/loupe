"""Exporters: where finished spans go.

OTLPHttpExporter batches spans on a background thread so instrumented code
never waits on the network:

    export() ──put_nowait──▶ [bounded queue] ──worker thread──▶ POST /v1/traces
                │ full?                         (batch_size spans or every
                ▼                                 `interval` seconds)
          drop + count (never block the agent)

The server may answer 429 when its own queue is full (backpressure); the
worker then retries with exponential backoff before giving up on that batch.
"""
from __future__ import annotations

import json
import queue
import threading
import time
import urllib.error
import urllib.request
from collections import defaultdict


class OTLPHttpExporter:
    def __init__(self, endpoint: str = "http://127.0.0.1:7070", max_queue: int = 10_000,
                 batch_size: int = 256, interval: float = 0.5, timeout: float = 2.0, max_retries: int = 4):
        self.url = endpoint.rstrip("/") + "/v1/traces"
        self.batch_size = batch_size
        self.interval = interval
        self.timeout = timeout
        self.max_retries = max_retries
        self._queue: queue.Queue = queue.Queue(maxsize=max_queue)
        self._stop = threading.Event()
        self._lock = threading.Lock()
        self.sent = 0       # spans the server accepted
        self.dropped = 0    # spans dropped because the local queue was full
        self.failed = 0     # spans lost after retries were exhausted
        self._worker = threading.Thread(target=self._run, name="loupe-exporter", daemon=True)
        self._worker.start()

    def export(self, span, service: str) -> None:
        try:
            self._queue.put_nowait((service, span.to_otlp()))
        except queue.Full:
            with self._lock:
                self.dropped += 1

    def _run(self) -> None:
        while not (self._stop.is_set() and self._queue.empty()):
            batch = []
            try:
                batch.append(self._queue.get(timeout=self.interval))
                while len(batch) < self.batch_size:
                    batch.append(self._queue.get_nowait())
            except queue.Empty:
                pass
            if batch:
                self._send(batch)
                for _ in batch:
                    self._queue.task_done()

    def _send(self, batch: list) -> None:
        by_service = defaultdict(list)
        for service, span in batch:
            by_service[service].append(span)
        body = {"resourceSpans": [
            {"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": svc}}]},
             "scopeSpans": [{"scope": {"name": "loupe_sdk"}, "spans": spans}]}
            for svc, spans in by_service.items()
        ]}
        data = json.dumps(body).encode()
        for attempt in range(self.max_retries + 1):
            try:
                req = urllib.request.Request(self.url, data=data, method="POST",
                                             headers={"Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    resp.read()
                with self._lock:
                    self.sent += len(batch)
                return
            except urllib.error.HTTPError as e:
                if e.code != 429 and e.code < 500:
                    break  # a 4xx other than 429 will not succeed on retry
            except (urllib.error.URLError, OSError):
                pass
            time.sleep(min(0.1 * 2 ** attempt, 2.0))
        with self._lock:
            self.failed += len(batch)

    def flush(self, timeout: float = 5.0) -> bool:
        """Wait until everything queued so far has been sent (or given up on)."""
        deadline = time.monotonic() + timeout
        while self._queue.unfinished_tasks and time.monotonic() < deadline:
            time.sleep(0.01)
        return self._queue.unfinished_tasks == 0

    def shutdown(self, timeout: float = 5.0) -> None:
        self.flush(timeout)
        self._stop.set()
        self._worker.join(timeout)


class FileExporter:
    """Appends spans as JSON lines (Loupe native format) — offline runs, then
    `bin/loupe ingest-file spans.jsonl`."""

    def __init__(self, path: str):
        self.path = path
        self._lock = threading.Lock()

    def export(self, span, service: str) -> None:
        line = json.dumps(span.to_native(service), ensure_ascii=False)
        with self._lock, open(self.path, "a", encoding="utf-8") as fh:
            fh.write(line + "\n")

    def flush(self, timeout: float = 5.0) -> bool:
        return True

    def shutdown(self, timeout: float = 5.0) -> None:
        pass


class InMemoryExporter:
    """Keeps finished spans in a list (tests)."""

    def __init__(self):
        self.spans = []

    def export(self, span, service: str) -> None:
        self.spans.append(span)

    def flush(self, timeout: float = 5.0) -> bool:
        return True

    def shutdown(self, timeout: float = 5.0) -> None:
        pass


class MultiExporter:
    """Fan out to several exporters (e.g. file + server)."""

    def __init__(self, *exporters):
        self.exporters = exporters

    def export(self, span, service: str) -> None:
        for e in self.exporters:
            e.export(span, service)

    def flush(self, timeout: float = 5.0) -> bool:
        return all(e.flush(timeout) for e in self.exporters)

    def shutdown(self, timeout: float = 5.0) -> None:
        for e in self.exporters:
            e.shutdown(timeout)
