#!/usr/bin/env python3
"""Stock-Pi queue E2E fixture for task5906 (Plan3 Task3).

Starts the installed, unmodified Pi exactly 1.4.0 in an isolated PTY with the
private default agent home and the authentic sessions/--encoded-cwd-- layout.
Extension load order is pinned: controlled public input handler first,
deterministic provider/observer second, production Rocket bridge third.

The fixture prints exactly one JSON line with the private paths and identity,
then waits for stdin EOF or SIGTERM. Only its own child process group is
touched; the PTY is drained continuously.

Run by the opt-in Go test; not a manual probe.
"""

import json
import os
import pty
import re
import secrets
import select
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
INPUT_HANDLED = ROOT / "scripts" / "probes" / "tmux-delivary" / "input-handled-extension.ts"
OBSERVER = ROOT / "scripts" / "probes" / "tmux-delivary" / "native-bridge-probe-extension.ts"
PRODUCTION = ROOT / "internal" / "agent" / "pi" / "delivery-extension.ts"
REQUIRED_PI = "1.4.0"


def fail(message):
    print(f"pi_queue_e2e_fixture: {message}", file=sys.stderr, flush=True)
    raise SystemExit(2)


def scrub_env():
    env = os.environ.copy()
    credential = re.compile(r"(_API_KEY|_AUTH_TOKEN|_OAUTH_TOKEN|_ACCESS_TOKEN)$", re.IGNORECASE)
    provider = re.compile(
        r"^(DEEPSEEK|OPENAI|ANTHROPIC|GOOGLE|GEMINI|OPENROUTER|XAI|GROQ|MISTRAL|AZURE|AWS|COPILOT|ZAI|KIMI|MOONSHOT)",
        re.IGNORECASE,
    )
    for key in [key for key in env if credential.search(key) or provider.match(key)]:
        del env[key]
    return env


def probe_rpc(path, request, timeout=5):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(timeout)
        client.connect(str(path))
        client.sendall(json.dumps(request, ensure_ascii=False).encode() + b"\n")
        data = b""
        while not data.endswith(b"\n"):
            part = client.recv(65536)
            if not part:
                break
            data += part
    return json.loads(data)


def wait_until(predicate, timeout, description, process, output):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"Pi exited {process.returncode}: {output()[-4000:]}")
        if predicate():
            return
        time.sleep(0.05)
    raise TimeoutError(f"{description}; Pi tail: {output()[-4000:]}")


class Fixture:
    def __init__(self):
        self.dir = Path(tempfile.mkdtemp(prefix="rpi-e2e-", dir="/tmp"))
        os.chmod(self.dir, 0o700)
        self.agent_home = self.dir / "agent"
        self.cwd = self.dir / "cwd"
        self.agent_home.mkdir(parents=True, exist_ok=True)
        self.cwd.mkdir(parents=True, exist_ok=True)
        self.endpoint = self.dir / "s"
        self.probe_socket = self.dir / "p"
        self.input_log = self.dir / "input.jsonl"
        self.probe_log = self.dir / "probe-events.jsonl"
        self.session_id = "e2e-" + secrets.token_hex(6)
        self.instance_id = secrets.token_hex(8)
        self.output = ""
        self.lock = threading.Lock()
        self.stop_reader = threading.Event()
        self.process = None
        self.master = None
        self.reader = None

    def start(self):
        version = subprocess.check_output(["pi", "--version"], text=True).strip()
        if version != REQUIRED_PI:
            fail(f"installed Pi is {version}, exactly {REQUIRED_PI} is required")
        for path in (INPUT_HANDLED, OBSERVER, PRODUCTION):
            if not path.exists():
                fail(f"missing fixture or production extension: {path}")

        env = scrub_env()
        env.update({
            "PI_OFFLINE": "1",
            "PI_SKIP_VERSION_CHECK": "1",
            "PI_TELEMETRY": "0",
            "PI_CODING_AGENT_DIR": str(self.agent_home),
            "PI_PROBE_SOCKET": str(self.probe_socket),
            "PI_PROBE_LOG": str(self.probe_log),
            "PI_PROBE_DELAY_MS": "150",
            "PI_INPUT_HANDLED_LOG": str(self.input_log),
            "PI_INPUT_HANDLED_FIRST": "1",
            "ROCKET_PI_NATIVE_SESSION_ID": self.session_id,
            "ROCKET_PI_NATIVE_INSTANCE_ID": self.instance_id,
            "ROCKET_PI_NATIVE_ENDPOINT": str(self.endpoint),
            "ROCKET_PI_NATIVE_PROTOCOL": "1",
            "TERM": "xterm-256color",
        })
        command = [
            "pi", "--offline", "--no-extensions", "--no-mcp", "--no-skills",
            "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-tools",
            "--model", "probe/deterministic",
            "--session-id", self.session_id,
            # Pinned load order: controlled handler, observer/provider, production.
            "--extension", str(INPUT_HANDLED),
            "--extension", str(OBSERVER),
            "--extension", str(PRODUCTION),
        ]
        master, slave = pty.openpty()
        import fcntl
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        # Pi stays in the fixture's own process group (Go creates it with
        # Setpgid) so the owner can always reap the whole group. No separate
        # session and no bare-PID fallback are used.
        self.process = subprocess.Popen(
            command, cwd=self.cwd, env=env, stdin=slave, stdout=slave, stderr=slave,
            close_fds=True,
        )
        os.close(slave)
        self.master = master
        self.reader = threading.Thread(target=self._drain, daemon=True)
        self.reader.start()
        if os.environ.get("PI_QUEUE_E2E_FAIL_AFTER_SPAWN") == "1":
            # Deterministic pre-ready failure for the cleanup regression:
            # announce the private resources, then fail before readiness.
            print(json.dumps({"fixture_dir": str(self.dir), "pi_pid": self.process.pid}), file=sys.stderr, flush=True)
            raise RuntimeError("synthetic pre-ready failure")
        wait_until(
            lambda: self.endpoint.exists() and self.probe_socket.exists(),
            25, "native and probe sockets", self.process, self.tail,
        )

    def _drain(self):
        while not self.stop_reader.is_set():
            try:
                ready, _, _ = select.select([self.master], [], [], 0.1)
            except (OSError, ValueError):
                return
            if not ready:
                continue
            try:
                chunk = os.read(self.master, 65536)
            except OSError:
                return
            if not chunk:
                return
            with self.lock:
                self.output += chunk.decode("utf-8", errors="replace")

    def tail(self):
        with self.lock:
            return self.output

    def info(self):
        return {
            "pi_version": REQUIRED_PI,
            "agent_home": str(self.agent_home),
            "cwd": str(self.cwd),
            "session_id": self.session_id,
            "instance_id": self.instance_id,
            "endpoint": str(self.endpoint),
            "input_log": str(self.input_log),
            "probe_log": str(self.probe_log),
            "pi_pid": self.process.pid,
        }

    def close(self):
        if self.process is not None:
            try:
                probe_rpc(self.probe_socket, {"op": "shutdown"}, timeout=2)
            except Exception:
                pass
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)
        self.stop_reader.set()
        if self.reader is not None:
            self.reader.join(timeout=2)
        if self.master is not None:
            try:
                os.close(self.master)
            except OSError:
                pass
        shutil.rmtree(self.dir, ignore_errors=True)


def _exit_on_signal(signum, frame):
    raise SystemExit(0)


def main():
    # Installed before Pi is spawned so an early TERM/INT still runs the
    # finally cleanup instead of leaving an orphan Pi or temp directory.
    signal.signal(signal.SIGTERM, _exit_on_signal)
    signal.signal(signal.SIGINT, _exit_on_signal)
    fixture = Fixture()
    try:
        fixture.start()
        print(json.dumps(fixture.info(), ensure_ascii=False), flush=True)
        # The Go test closes stdin when it is done with the run.
        try:
            sys.stdin.buffer.read()
        except KeyboardInterrupt:
            pass
    finally:
        fixture.close()


if __name__ == "__main__":
    main()
