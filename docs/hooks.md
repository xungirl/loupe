# Streaming Claude Code into Loupe (hooks) and importing past sessions

Two ways to get your Claude Code activity into Loupe. They use the same span
ids, so you can use both on the same session without duplicates.

| | Live hooks | Transcript import |
|---|---|---|
| What | `importers/claude_hook.py` runs on each hook event and POSTs spans | `loupe import-claude` reads `~/.claude/projects/**/*.jsonl` |
| When | while you work (shows up in **Live tail**) | any time, for past sessions |
| Tokens / cost | no (hooks don't carry token usage) | yes (from `message.usage`) |
| Subagent tree | no (all hook spans hang off the root) | yes (Task/Agent calls → subagent sidechains) |
| Needs the server running | yes | no (writes the SQLite file directly) |

Nothing here was run against your real `~/.claude` by the author: the importer
was tested on synthetic fixtures only (`server/internal/claudeimport/testdata`).
Run it yourself and read the parse stats it prints.

## 1. Import your past sessions

```bash
make import-claude                      # reads ~/.claude/projects, writes data/local.db (gitignored)
make serve-local                        # opens the UI on that database at http://127.0.0.1:7070
```

Under the hood:

```bash
bin/loupe import-claude -dir ~/.claude/projects -db data/local.db
bin/loupe serve -db data/local.db -web web/dist
```

The importer prints parse stats like `malformed`, `truncated`, `skipped_types`,
`missing_tool_results`, `subagents_unlinked`. Non-zero values are not fatal —
they tell you which parts of a newer/older transcript format Loupe did not
understand. Re-running the import is safe: existing spans are skipped
(idempotent by span id), new sessions are added.

`data/` is in `.gitignore`. Your transcripts contain your code and prompts;
keep the database local.

## 2. Live streaming with hooks

Start the server first (`make demo` or `bin/loupe serve -web web/dist`), then add
this to `~/.claude/settings.json` (user-wide) or `.claude/settings.json` (one
project). Replace the path if you cloned Loupe somewhere else.

```json
{
  "hooks": {
    "SessionStart":     [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "PreToolUse":       [{ "matcher": "*", "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "PostToolUse":      [{ "matcher": "*", "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "Notification":     [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "Stop":             [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "SubagentStop":     [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }],
    "SessionEnd":       [{ "hooks": [{ "type": "command", "command": "python3 /path/to/loupe/importers/claude_hook.py", "timeout": 5 }] }]
  }
}
```

If you already have a `"hooks"` block, merge these entries into it instead of
replacing it. Open the **Live** page in the UI and use Claude Code; tool calls
appear as they finish.

### What the hook does (and doesn't)

* Reads the hook JSON from stdin (`session_id`, `hook_event_name`, `tool_name`,
  `tool_input`, `tool_response`, `tool_use_id`, `prompt`, ...).
* `PreToolUse` stores the start time in `$TMPDIR/loupe-hook/<session>/`;
  `PostToolUse` emits a `tool` span with the real duration and deletes it.
* Other events become zero-duration `event` spans.
* Every request also carries the session's `root` span; the server drops the
  duplicates, so a trace always has a root even if you enable hooks mid-session.
* Always exits 0, prints nothing to stdout, and gives up after 0.5 s if the
  server is down — Claude Code is never blocked or changed by it.

Environment knobs: `LOUPE_URL` (default `http://127.0.0.1:7070`),
`LOUPE_HOOK_TIMEOUT` (seconds), `LOUPE_HOOK_STATE`, and `LOUPE_HOOK_LOG=/tmp/loupe-hook.log`
to debug.

### Removing it

Delete the entries from `settings.json`. Nothing else is installed.
