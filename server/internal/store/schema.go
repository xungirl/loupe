package store

// schema is applied on every Open; every statement is idempotent.
//
// Design notes:
//   - spans is WITHOUT ROWID with PRIMARY KEY (trace_id, span_id): fetching a
//     whole trace is a single range scan on the primary key, and duplicate span
//     IDs are rejected by the key itself (INSERT OR IGNORE = idempotent ingest).
//   - traces is a summary table maintained incrementally by the writer, so the
//     sessions list never has to aggregate millions of span rows.
//   - findings are owned by detectors and rewritten wholesale per trace.
const schema = `
CREATE TABLE IF NOT EXISTS spans (
	trace_id           TEXT NOT NULL,
	span_id            TEXT NOT NULL,
	parent_span_id     TEXT NOT NULL DEFAULT '',
	kind               TEXT NOT NULL,
	name               TEXT NOT NULL,
	source             TEXT NOT NULL DEFAULT '',
	agent_id           TEXT NOT NULL DEFAULT '',
	agent_name         TEXT NOT NULL DEFAULT '',
	start_ns           INTEGER NOT NULL,
	end_ns             INTEGER NOT NULL,
	status             TEXT NOT NULL DEFAULT 'ok',
	error_msg          TEXT NOT NULL DEFAULT '',
	model              TEXT NOT NULL DEFAULT '',
	input_tokens       INTEGER NOT NULL DEFAULT 0,
	output_tokens      INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	cost_usd           REAL NOT NULL DEFAULT 0,
	tool_name          TEXT NOT NULL DEFAULT '',
	target             TEXT NOT NULL DEFAULT '',
	tool_input         TEXT NOT NULL DEFAULT '',
	tool_output        TEXT NOT NULL DEFAULT '',
	attributes         TEXT NOT NULL DEFAULT '',
	ingested_ns        INTEGER NOT NULL,
	PRIMARY KEY (trace_id, span_id)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_spans_tool ON spans(tool_name, status) WHERE tool_name != '';

CREATE TABLE IF NOT EXISTS traces (
	trace_id           TEXT PRIMARY KEY,
	name               TEXT NOT NULL DEFAULT '',
	source             TEXT NOT NULL DEFAULT '',
	start_ns           INTEGER NOT NULL,
	end_ns             INTEGER NOT NULL,
	span_count         INTEGER NOT NULL DEFAULT 0,
	error_count        INTEGER NOT NULL DEFAULT 0,
	llm_calls          INTEGER NOT NULL DEFAULT 0,
	tool_calls         INTEGER NOT NULL DEFAULT 0,
	agent_count        INTEGER NOT NULL DEFAULT 0,
	input_tokens       INTEGER NOT NULL DEFAULT 0,
	output_tokens      INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	cost_usd           REAL NOT NULL DEFAULT 0,
	last_ingest_ns     INTEGER NOT NULL,
	detected_ns        INTEGER NOT NULL DEFAULT 0,
	finding_count      INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_traces_start ON traces(start_ns DESC);
CREATE INDEX IF NOT EXISTS idx_traces_last_ingest ON traces(last_ingest_ns);

CREATE TABLE IF NOT EXISTS findings (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	trace_id    TEXT NOT NULL,
	detector    TEXT NOT NULL,
	severity    TEXT NOT NULL,
	agent_id    TEXT NOT NULL DEFAULT '',
	summary     TEXT NOT NULL,
	span_ids    TEXT NOT NULL DEFAULT '[]',
	score       REAL NOT NULL DEFAULT 0,
	created_ns  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_findings_trace ON findings(trace_id);
CREATE INDEX IF NOT EXISTS idx_findings_detector ON findings(detector);
`
