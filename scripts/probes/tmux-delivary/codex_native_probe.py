#!/usr/bin/env python3
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
                message = json.loads(
                    await asyncio.wait_for(self.ws.recv(), remaining)
                )
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
            await self.ws.close()
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

    def send(self, data):
        os.write(self.master, data.encode("utf-8"))

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


async def queue(thread_id, text, socket_path, check=True):
    result = await asyncio.to_thread(
        subprocess.run,
        [
            "codex",
            "queue",
            "--thread",
            thread_id,
            "--message",
            text,
            "--remote",
            f"unix://{socket_path}",
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    output = (result.stdout + result.stderr).strip()
    if check and result.returncode != 0:
        raise RuntimeError(output)
    return {"exitCode": result.returncode, "output": output}


def turn_with_text(turns, marker):
    for turn in turns:
        for item in turn.get("items", []):
            if marker in json.dumps(item, ensure_ascii=False):
                return turn
    return None


def cursor_status(before, after_busy, after_restart):
    baseline = before["cursor"]["x"]
    if baseline >= 19:
        return "unverified_baseline_not_midline"
    positions = [baseline, after_busy["cursor"]["x"], after_restart["cursor"]["x"]]
    if len(set(positions)) == 1:
        return "preserved"
    return "moved"


async def main():
    parser = argparse.ArgumentParser(
        description="Bounded isolated Codex app-server and stock-TUI delivery probe"
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
    restarted = None
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
        tui.send("cursor-черновик-ε")
        tui.send("\x1b[D\x1b[D")
        await asyncio.sleep(1)
        evidence["cursorBefore"] = tui.snapshot()
        (root / "cursor-before.json").write_text(
            json.dumps(evidence["cursorBefore"], ensure_ascii=False, indent=2) + "\n"
        )
        long_prompt = (
            "Run this exact shell command: printf 'ROCKET_SESSION_ID=%s\\nROCKET_SOCKET=%s\\n' "
            '"$ROCKET_SESSION_ID" "$ROCKET_SOCKET"; sleep 3. Then reply with exactly BUSY_DONE'
        )
        evidence["longQueue"] = await queue(thread_id, long_prompt, server.socket_path)
        await asyncio.sleep(1)
        evidence["busyQueue"] = await queue(
            thread_id, "ROCKET_BUSY: reply with exactly BUSY_OK", server.socket_path
        )
        long_turn = None
        busy_turn = None
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            tui.pump()
            turns = (
                await server.request(
                    "thread/turns/list", {"threadId": thread_id, "itemsView": "full"}
                )
            )["data"]
            long_turn = turn_with_text(turns, "BUSY_DONE")
            busy_turn = turn_with_text(turns, "BUSY_OK")
            if (
                long_turn
                and busy_turn
                and long_turn["status"] == busy_turn["status"] == "completed"
            ):
                break
            await asyncio.sleep(0.5)
        else:
            raise TimeoutError("busy turns did not complete")
        evidence["longTurn"] = {
            "startedAt": long_turn["startedAt"],
            "completedAt": long_turn["completedAt"],
        }
        evidence["busyTurn"] = {
            "startedAt": busy_turn["startedAt"],
            "completedAt": busy_turn["completedAt"],
        }
        if long_turn["completedAt"] > busy_turn["startedAt"]:
            raise AssertionError("follow-up started before the active turn completed")
        executions = [
            item for item in long_turn["items"] if item.get("type") == "commandExecution"
        ]
        env_output = "\n".join(
            item.get("aggregatedOutput", "") for item in executions
        )
        expected_output = (
            f"ROCKET_SESSION_ID={args.session_id}\nROCKET_SOCKET={args.rocket_socket}\n"
        )
        if expected_output not in env_output:
            raise AssertionError(f"unexpected shell environment: {env_output}")
        evidence["shellOutput"] = env_output
        await wait_for_text(tui, "BUSY_OK")
        evidence["cursorAfterBusy"] = tui.snapshot()
        (root / "cursor-after-busy.json").write_text(
            json.dumps(evidence["cursorAfterBusy"], ensure_ascii=False, indent=2)
            + "\n"
        )
        server.stop()
        evidence["offlineQueue"] = await queue(
            thread_id, "ROCKET_OFFLINE: no blind retry", server.socket_path, check=False
        )
        if evidence["offlineQueue"]["exitCode"] == 0:
            raise AssertionError("offline queue unexpectedly succeeded")
        restarted = AppServer(root, args.session_id, args.rocket_socket)
        restarted.start()
        await restarted.connect()
        evidence["restartQueue"] = await queue(
            thread_id, "ROCKET_RESTART: reply with exactly RESTART_OK", restarted.socket_path
        )
        await wait_for_text(tui, "RESTART_OK")
        evidence["cursorAfterRestart"] = tui.snapshot()
        (root / "cursor-after-restart.json").write_text(
            json.dumps(evidence["cursorAfterRestart"], ensure_ascii=False, indent=2)
            + "\n"
        )
        evidence["draftPreserved"] = "cursor-черновик-ε" in tui.text()
        if not evidence["draftPreserved"]:
            raise AssertionError("draft disappeared from TUI")
        evidence["cursorStatus"] = cursor_status(
            evidence["cursorBefore"],
            evidence["cursorAfterBusy"],
            evidence["cursorAfterRestart"],
        )
        evidence["status"] = "completed"
    except Exception as error:
        evidence["status"] = "failed"
        evidence["error"] = str(error)
        raise
    finally:
        if tui is not None:
            tui.stop()
        if restarted is not None:
            await restarted.close()
        await server.close()
        (root / "evidence.json").write_text(
            json.dumps(evidence, ensure_ascii=False, indent=2) + "\n"
        )
        print(json.dumps(evidence, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    asyncio.run(main())
