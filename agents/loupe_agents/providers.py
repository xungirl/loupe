"""Model providers.

Both providers return the same Completion shape, so the agent code cannot tell
whether it is talking to a real model or the scripted fake.

OpenAICompatProvider talks to any OpenAI-compatible /chat/completions endpoint
(default: Alibaba DashScope compatible mode, model qwen3.8-flash). The API key
is read at call time from $DASHSCOPE_API_KEY or from the file named by
$LOUPE_KEYFILE — never from code, never logged.
"""
from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field

from .ledger import SpendLedger

DASHSCOPE_BASE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
DEFAULT_MODEL = "qwen3.8-flash"


class MissingAPIKey(RuntimeError):
    pass


@dataclass
class ToolCall:
    id: str
    name: str
    arguments: dict


@dataclass
class Completion:
    content: str
    tool_calls: list[ToolCall]
    input_tokens: int
    output_tokens: int
    model: str
    message: dict = field(default_factory=dict)  # assistant message to append to the history


def load_api_key() -> str:
    """$DASHSCOPE_API_KEY, else the contents of the file at $LOUPE_KEYFILE."""
    key = os.environ.get("DASHSCOPE_API_KEY", "").strip()
    if key:
        return key
    path = os.environ.get("LOUPE_KEYFILE", "").strip()
    if path:
        with open(os.path.expanduser(path)) as fh:
            key = fh.read().strip()
        if key:
            return key
    raise MissingAPIKey("set DASHSCOPE_API_KEY or LOUPE_KEYFILE=/path/to/keyfile")


def have_api_key() -> bool:
    try:
        load_api_key()
        return True
    except (MissingAPIKey, OSError):
        return False


def estimate_tokens(messages: list, tools: list | None) -> int:
    """Conservative prompt-size estimate for the budget check (chars / 3)."""
    return len(json.dumps(messages, ensure_ascii=False)) // 3 + (len(json.dumps(tools)) // 3 if tools else 0)


def parse_completion(body: dict, model: str) -> Completion:
    """Turn an OpenAI-style chat completion response into a Completion."""
    msg = body["choices"][0]["message"]
    calls = []
    for tc in msg.get("tool_calls") or []:
        fn = tc.get("function", {})
        try:
            args = json.loads(fn.get("arguments") or "{}")
            if not isinstance(args, dict):
                args = {"value": args}
        except json.JSONDecodeError:
            args = {"_unparsed": fn.get("arguments", "")}
        calls.append(ToolCall(id=tc.get("id", ""), name=fn.get("name", ""), arguments=args))
    usage = body.get("usage") or {}
    history_msg = {"role": "assistant", "content": msg.get("content") or ""}
    if msg.get("tool_calls"):
        history_msg["tool_calls"] = msg["tool_calls"]
    return Completion(content=msg.get("content") or "", tool_calls=calls,
                      input_tokens=int(usage.get("prompt_tokens", 0)), output_tokens=int(usage.get("completion_tokens", 0)),
                      model=body.get("model", model), message=history_msg)


class OpenAICompatProvider:
    def __init__(self, model: str = DEFAULT_MODEL, base_url: str = DASHSCOPE_BASE_URL,
                 ledger: SpendLedger | None = None, max_tokens: int = 1500, timeout: float = 90.0,
                 max_retries: int = 3, extra_body: dict | None = None):
        self.model = model
        self.base_url = base_url.rstrip("/")
        self.ledger = ledger
        self.max_tokens = max_tokens
        self.timeout = timeout
        self.max_retries = max_retries
        # Qwen3 models on DashScope's compatible mode need thinking disabled for
        # non-streaming calls; harmless for other OpenAI-compatible servers.
        self.extra_body = {"enable_thinking": False} if extra_body is None else extra_body

    def complete(self, messages: list, tools: list | None = None) -> Completion:
        key = load_api_key()
        if self.ledger is not None:
            self.ledger.check(estimate_tokens(messages, tools), self.max_tokens)  # may raise BudgetExceeded
        body = {"model": self.model, "messages": messages, "max_tokens": self.max_tokens, "temperature": 0,
                **self.extra_body}
        if tools:
            body["tools"] = tools
        data = json.dumps(body).encode()
        last_err: Exception | None = None
        for attempt in range(self.max_retries + 1):
            req = urllib.request.Request(self.base_url + "/chat/completions", data=data, method="POST",
                                         headers={"Content-Type": "application/json", "Authorization": f"Bearer {key}"})
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    out = json.loads(resp.read())
                comp = parse_completion(out, self.model)
                if self.ledger is not None:
                    self.ledger.record(comp.input_tokens, comp.output_tokens, {"model": comp.model})
                return comp
            except urllib.error.HTTPError as e:
                detail = e.read()[:300].decode(errors="replace")
                last_err = RuntimeError(f"HTTP {e.code}: {detail}")
                if e.code != 429 and e.code < 500:
                    break
            except (urllib.error.URLError, TimeoutError, OSError) as e:
                last_err = e
            time.sleep(min(2 ** attempt, 20))
        raise RuntimeError(f"model call failed after retries: {last_err}")
