"""Structured stdout contract: JSONL frames, graceful shutdown, exit codes.

Implements docs/design/engine-process-contract.md natively — next-integrated
emits contract frames without a wrapper layer.
"""

from __future__ import annotations

import json
import signal
import sys
import threading
import time
from typing import IO, Any, Optional


class Emitter:
    def __init__(self, stream: Optional[IO[str]] = None):
        self._stream = stream if stream is not None else sys.stdout
        self._lock = threading.Lock()

    def emit(self, type_: str, **fields: Any) -> None:
        frame = {"type": type_, **fields}
        line = json.dumps(frame, ensure_ascii=False)
        with self._lock:
            self._stream.write(line + "\n")
            self._stream.flush()

    def progress(self, step: int, total: int, loss: float | None = None,
                 lr: float | None = None, elapsed_s: float | None = None) -> None:
        fields: dict[str, Any] = {"step": step, "total": total}
        if loss is not None:
            fields["loss"] = loss
        if lr is not None:
            fields["lr"] = lr
        if elapsed_s is not None:
            fields["elapsed_s"] = elapsed_s
        self.emit("progress", **fields)

    def artifact(self, kind: str, path: str) -> None:
        self.emit("artifact", kind=kind, path=path)

    def log(self, level: str, msg: str) -> None:
        self.emit("log", level=level, msg=msg)

    def error(self, code: str, msg: str) -> None:
        self.emit("error", code=code, msg=msg)


class Shutdown:
    """SIGTERM/SIGINT flag: engines poll `requested` and checkpoint at step
    boundaries (尽力而为); the agent's SIGKILL is the backstop."""

    def __init__(self):
        self.requested = False
        self._signal: int | None = None
        signal.signal(signal.SIGTERM, self._on_signal)
        signal.signal(signal.SIGINT, self._on_signal)

    def _on_signal(self, signum: int, _frame: Any) -> None:
        self.requested = True
        self._signal = signum


class Clock:
    def __init__(self):
        self._t0 = time.monotonic()

    @property
    def elapsed_s(self) -> float:
        return round(time.monotonic() - self._t0, 3)


EXIT_OK = 0
EXIT_ERROR = 1
