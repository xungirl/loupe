"""The agent's tools: a sandboxed workspace directory.

Tools return (text, ok). Errors are returned to the model as text instead of
raised, because a tool failing is normal agent life (the model should read
the error and react), not a crash.
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile

TOOL_SCHEMAS = [
    {"type": "function", "function": {
        "name": "list_files", "description": "List files in the workspace.",
        "parameters": {"type": "object", "properties": {}, "required": []}}},
    {"type": "function", "function": {
        "name": "read_file", "description": "Read a text file from the workspace.",
        "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}},
    {"type": "function", "function": {
        "name": "write_file", "description": "Create or overwrite a file in the workspace with the full content.",
        "parameters": {"type": "object", "properties": {"path": {"type": "string"}, "content": {"type": "string"}},
                       "required": ["path", "content"]}}},
    {"type": "function", "function": {
        "name": "run_tests", "description": "Run the visible unit tests (test_visible.py) and return the output.",
        "parameters": {"type": "object", "properties": {}, "required": []}}},
]

MAX_OUTPUT = 4000


class Workspace:
    """A temp directory the agent may read and write, nothing else."""

    def __init__(self, files: dict[str, str], test_timeout: float = 10.0):
        self.root = tempfile.mkdtemp(prefix="loupe-ws-")
        self.test_timeout = test_timeout
        for name, content in files.items():
            with open(os.path.join(self.root, name), "w") as fh:
                fh.write(content)

    def close(self) -> None:
        shutil.rmtree(self.root, ignore_errors=True)

    def _path(self, rel: str) -> str:
        full = os.path.realpath(os.path.join(self.root, rel))
        if not full.startswith(os.path.realpath(self.root) + os.sep):
            raise PermissionError(f"path escapes the workspace: {rel}")
        return full

    def list_files(self) -> tuple[str, bool]:
        return "\n".join(sorted(os.listdir(self.root))), True

    def read_file(self, path: str) -> tuple[str, bool]:
        try:
            with open(self._path(path)) as fh:
                return fh.read()[:MAX_OUTPUT], True
        except (OSError, PermissionError) as e:
            return f"Error: {e}", False

    def write_file(self, path: str, content: str) -> tuple[str, bool]:
        try:
            with open(self._path(path), "w") as fh:
                fh.write(content)
            return f"wrote {len(content)} bytes to {path}", True
        except (OSError, PermissionError) as e:
            return f"Error: {e}", False

    def run_tests(self, test_file: str = "test_visible.py", timeout: float | None = None) -> tuple[str, bool]:
        """Run a unittest file in a fresh interpreter with a timeout."""
        try:
            proc = subprocess.run([sys.executable, "-E", "-s", "-m", "unittest", "-q", test_file.removesuffix(".py")],
                                  cwd=self.root, capture_output=True, text=True,
                                  timeout=timeout or self.test_timeout, env={"PATH": os.environ.get("PATH", "")})
        except subprocess.TimeoutExpired:
            return f"Error: tests timed out after {timeout or self.test_timeout}s", False
        out = (proc.stdout + proc.stderr).strip()
        return out[-MAX_OUTPUT:], proc.returncode == 0

    def call(self, name: str, args: dict) -> tuple[str, bool]:
        """Dispatch a tool call coming from the model."""
        try:
            if name == "list_files":
                return self.list_files()
            if name == "read_file":
                return self.read_file(str(args["path"]))
            if name == "write_file":
                return self.write_file(str(args["path"]), str(args["content"]))
            if name == "run_tests":
                return self.run_tests()
        except KeyError as e:
            return f"Error: missing argument {e}", False
        return f"Error: unknown tool {name}", False
