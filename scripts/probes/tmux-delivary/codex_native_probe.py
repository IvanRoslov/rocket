#!/usr/bin/env python3
"""Minimal isolated Codex observer-completion probe.

This probe starts a private Codex app-server, connects the stock TUI to it
over a private Unix socket, starts one model turn, and waits for the
`turn/completed` notification. It exists to reproduce the documented
observer limitation: the rendered TUI can show a finished turn while the
observer connection never receives `turn/completed`.

It deliberately does NOT try to prove cursor preservation, draft
preservation, busy follow-up, or restart delivery. Those scenarios need a
separate, properly executed gate; keeping an unexecuted success path here
would risk reporting a false PASS.

Cleanup stops only this probe's own child process groups. It never touches
a shared app-server daemon, global config/auth, or other sessions.
"""

import argparse
import asyncio
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import termios
import time
from pathlib import Path

import pyte
import websockets


class AppServer:
    def __init__(self, root, session_id, rocket_socket):
        self.root = Path(root)
        self.session_id = session_id
        self.rocket_socket = rocket_socket
        self.process = None
        self.ws = None
        self.next_id = 0
        self.notifications = []

    @property
    def socket_path(self):
        return self.root / "app.sock"

    def start(self):
        env = os.environ.copy()
        env["ROCKET_SESSION_ID"] = self.session_id
        env["ROCKET_SOCKET"] = self.rocket_socket
        log = (self.root / "server.log").open("ab")
        self.process = subprocess.Popen(
            ["codex", "app-server", "--listen", f"unix://{self.socket_path}"],
            cwd=self.root / "work",
            env=env,
            stdout=log,
            stderr=log,
            start_new_session=True,
        )

    async def connect(self):
        deadline = time.monotonic() + 30
        while True:
            try:
                self.ws = await websockets.unix_connect(
                    path=str(self.socket_path), max_size=64 * 1024 * 1024
                )
                break
            except (FileNotFoundError, ConnectionRefusedError, OSError):
                if time.monotonic() > deadline:
                    raise
                await asyncio.sleep(0.1)
        await self.request(
            "initialize",
            {
                "clientInfo": {
                    "name": "rocket_native_probe",
                    "title": "Rocket native delivery probe",
                    "version": "1.0",
                },
                "capabilities": {"experimentalApi": True},
            },
        )
        await self.ws.send(json.dumps({"method": "initialized", "params": {}}))

    async def request(self, method, params, timeout=60):
        self.next_id += 1
        request_id = self.next_id
        await self.ws.send(
            json.dumps({"method": method, "id": request_id, "params": params})
        )
        while True:
            message = json.loads(await asyncio.wait_for(self.ws.recv(), timeout))
            if message.get("id") == request_id:
                if "error" in message:
                    raise RuntimeError(message["error"])
                return message.get("result")
            self.notifications.append(message)

    async def wait_turn_completed(self, tui, turn_id, timeout=120):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            tui.pump()
            for message in self.notifications:
                if (
                    message.get("method") == "turn/completed"
                    and message.get("params", {}).get("turn", {}).get("id") == turn_id
                ):
                    return message["params"]["turn"]
            remaining = min(0.25, deadline - time.monotonic())
            if remaining <= 0:
                break
            try:
                message = json.loads(await asyncio.wait_for(self.ws.recv(), remaining))
            except asyncio.TimeoutError:
                continue
            self.notifications.append(message)
        raise TimeoutError(f"turn {turn_id} did not complete")

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGTERM)
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait()

    async def close(self):
        if self.ws is not None:
            try:
                await self.ws.close()
            except Exception:
                pass
        self.stop()


class Tui:
    def __init__(self, root, session_id, rocket_socket):
        self.root = Path(root)
        self.raw = b""
        self.log_path = self.root / "tui.log"
        env = os.environ.copy()
        env.update(
            {
                "TERM": "xterm-256color",
                "COLUMNS": "120",
                "LINES": "40",
                "ROCKET_SESSION_ID": session_id,
                "ROCKET_SOCKET": rocket_socket,
            }
        )
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        os.set_blocking(self.master, False)
        self.process = subprocess.Popen(
            [
                "codex",
                "--remote",
                f"unix://{self.root / 'app.sock'}",
                "--no-alt-screen",
                "-c",
                "check_for_update_on_startup=false",
            ],
            cwd=self.root / "work",
            env=env,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            start_new_session=True,
        )
        os.close(slave)

    def pump(self):
        while True:
            ready, _, _ = select.select([self.master], [], [], 0)
            if not ready:
                return
            try:
                chunk = os.read(self.master, 65536)
            except (BlockingIOError, OSError):
                return
            if not chunk:
                return
            self.raw += chunk
            with self.log_path.open("ab") as log:
                log.write(chunk)

    def text(self):
        self.pump()
        return self.raw.decode("utf-8", errors="replace")

    def snapshot(self):
        screen = pyte.Screen(120, 40)
        stream = pyte.Stream(screen)
        stream.feed(self.text())
        return {
            "cursor": {"x": screen.cursor.x, "y": screen.cursor.y},
            "lines": list(screen.display),
        }

    def stop(self):
        os.close(self.master)
        if self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGTERM)
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait()


async def wait_for_text(tui, marker, timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if marker in tui.text():
            return
        await asyncio.sleep(0.1)
    raise TimeoutError(f"marker not seen in TUI: {marker}")


async def main():
    parser = argparse.ArgumentParser(
        description="Minimal isolated Codex observer-completion probe"
    )
    parser.add_argument("--root", required=True)
    parser.add_argument("--session-id", required=True)
    parser.add_argument("--rocket-socket", required=True)
    args = parser.parse_args()
    root = Path(args.root)
    if root.exists() and any(root.iterdir()):
        raise SystemExit(f"refusing non-empty probe root: {root}")
    (root / "work").mkdir(parents=True, exist_ok=True)
    server = AppServer(root, args.session_id, args.rocket_socket)
    tui = None
    evidence = {
        "codexVersion": subprocess.run(
            ["codex", "--version"], capture_output=True, text=True, timeout=10
        ).stdout.strip(),
        "sessionId": args.session_id,
        "rocketSocket": args.rocket_socket,
    }
    try:
        server.start()
        await server.connect()
        tui = Tui(root, args.session_id, args.rocket_socket)
        await wait_for_text(tui, "GPT-")
        await asyncio.sleep(2)
        loaded = await server.request("thread/loaded/list", {})
        if len(loaded["data"]) != 1:
            raise RuntimeError(f"expected one live TUI thread, got {loaded['data']}")
        thread_id = loaded["data"][0]
        evidence["threadId"] = thread_id
        initial = await server.request(
            "turn/start",
            {
                "threadId": thread_id,
                "input": [{"type": "text", "text": "Reply with exactly PROBE_READY"}],
            },
        )
        await server.wait_turn_completed(tui, initial["turn"]["id"])
        evidence["status"] = "completed"
    except Exception as error:
        evidence["status"] = "failed"
        evidence["error"] = str(error)
        if tui is not None:
            evidence["renderedTui"] = tui.snapshot()
        raise
    finally:
        if tui is not None:
            tui.stop()
        await server.close()
        (root / "evidence.json").write_text(
            json.dumps(evidence, ensure_ascii=False, indent=2) + "\n"
        )
        print(json.dumps(evidence, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    asyncio.run(main())
