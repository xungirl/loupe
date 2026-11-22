"""Spend ledger: a hard budget stop for real model calls.

Before every call the provider asks the ledger whether the *worst case* cost
of that call (prompt tokens estimated from the request + the max_tokens cap
for the answer) still fits under the cap. If not, BudgetExceeded is raised and
no request is sent. After the call, the actual usage is recorded.

State is a small JSON file written atomically (write temp file → os.replace),
so a crash never leaves a half-written ledger and a resumed experiment sees
exactly what was already spent.
"""
from __future__ import annotations

import json
import os
import tempfile
import threading
import time
from dataclasses import dataclass


class BudgetExceeded(RuntimeError):
    """Raised before a call that could push spending over the cap."""


@dataclass(frozen=True)
class Prices:
    input_per_m: float = 0.8   # ¥ per 1M input tokens (qwen3.8-flash, given for this project)
    output_per_m: float = 2.7  # ¥ per 1M output tokens
    currency: str = "CNY"

    def cost(self, input_tokens: int, output_tokens: int) -> float:
        return (input_tokens * self.input_per_m + output_tokens * self.output_per_m) / 1_000_000


# How the guarantee works and where it ends: check() uses a conservative
# prompt estimate (characters / 3, see providers.estimate_tokens) plus the full
# max_tokens for the answer. The only way to overshoot the cap is a single call
# whose billed prompt tokens exceed that estimate; record() then makes the very
# next check() fail, so the overshoot is bounded by one call.
class SpendLedger:
    def __init__(self, path: str, cap: float = 4.50, prices: Prices = Prices()):
        self.path = path
        self.cap = cap
        self.prices = prices
        self._lock = threading.Lock()
        self.state = {"spent": 0.0, "calls": 0, "input_tokens": 0, "output_tokens": 0, "cap": cap,
                      "currency": prices.currency, "entries": []}
        if os.path.exists(path):
            with open(path) as fh:
                self.state.update(json.load(fh))
            self.state["cap"] = cap  # the cap passed now wins (you can lower it on resume)

    @property
    def spent(self) -> float:
        return self.state["spent"]

    @property
    def remaining(self) -> float:
        return max(0.0, self.cap - self.spent)

    def check(self, est_input_tokens: int, max_output_tokens: int) -> float:
        """Raise BudgetExceeded unless the worst case of the next call fits."""
        worst = self.prices.cost(est_input_tokens, max_output_tokens)
        with self._lock:
            if self.state["spent"] + worst > self.cap:
                raise BudgetExceeded(
                    f"next call could cost up to {worst:.4f} {self.prices.currency}; "
                    f"spent {self.state['spent']:.4f} of cap {self.cap:.2f}")
        return worst

    def record(self, input_tokens: int, output_tokens: int, meta: dict | None = None) -> float:
        cost = self.prices.cost(input_tokens, output_tokens)
        with self._lock:
            s = self.state
            s["spent"] = round(s["spent"] + cost, 8)
            s["calls"] += 1
            s["input_tokens"] += input_tokens
            s["output_tokens"] += output_tokens
            s["entries"].append({"t": time.time(), "in": input_tokens, "out": output_tokens,
                                 "cost": round(cost, 8), **(meta or {})})
            self._save()
        return cost

    def _save(self) -> None:
        d = os.path.dirname(os.path.abspath(self.path))
        os.makedirs(d, exist_ok=True)
        fd, tmp = tempfile.mkstemp(dir=d, prefix=".ledger-")
        with os.fdopen(fd, "w") as fh:
            json.dump(self.state, fh, indent=1)
        os.replace(tmp, self.path)  # atomic on POSIX
