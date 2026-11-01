#!/usr/bin/env python3
import argparse
import fcntl
import json
import os
import pty
import select
import shutil
import signal
import socket
import struct
import subprocess
import termios
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
EXTENSION = ROOT / "preservation-extension.ts"
SCENARIOS = [
    ("idle-single", False, "Черновик одной строки"),
    ("idle-multiline", False, "Первая строка\nВторая строка черновика"),
    ("busy-single", True, "Черновик во время работы"),
    ("busy-multiline", True, "Первая busy строка\nВторая busy строка"),
]
PI_VERSION = subprocess.check_output(["pi", "--version"], text=True).strip()
if PI_VERSION != "1.4.0":
    raise RuntimeError(f"Pi 1.4.0 is required, found {PI_VERSION}")


def wait_for(predicate, timeout, description, output, read_available):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        read_available()
        value = predicate()
        if value:
            return value
        time.sleep(0.05)
    read_available()
    raise TimeoutError(f"{description}; PTY tail: {output[-4000:]}")


def run_scenario(name, busy, draft):
    scenario_dir = ROOT / "results" / name
    if scenario_dir.exists():
        shutil.rmtree(scenario_dir)
    cwd = scenario_dir / "cwd"
    agent_dir = scenario_dir / "agent"
    sessions_dir = scenario_dir / "sessions"
    for directory in (cwd, agent_dir, sessions_dir):
        directory.mkdir(parents=True, exist_ok=True)
    socket_path = Path.cwd() / f".pi-probe-{name}.sock"
    socket_path.unlink(missing_ok=True)
    event_log = scenario_dir / "events.jsonl"
    pty_log = scenario_dir / "pty.raw"
    env = os.environ.copy()
    env.update({
        "PI_CODING_AGENT_DIR": str(agent_dir),
        "PI_CODING_AGENT_SESSION_DIR": str(sessions_dir),
        "PI_OFFLINE": "1",
        "PI_TELEMETRY": "0",
        "PI_PROBE_SOCKET": str(socket_path),
        "PI_PROBE_LOG": str(event_log),
        "PI_PROBE_DELAY_MS": "2500" if busy else "100",
        "TERM": "xterm-256color",
    })
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    command = [
        "pi", "--offline", "--no-extensions", "--no-mcp", "--no-skills",
        "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-tools",
        "--model", "probe/deterministic", "--session-dir", str(sessions_dir),
        "--extension", str(EXTENSION),
    ]
    process = subprocess.Popen(
        command, cwd=cwd, env=env, stdin=slave, stdout=slave, stderr=slave,
        start_new_session=True, close_fds=True,
    )
    os.close(slave)
    output = ""
    output_lock = threading.Lock()
    stop_reader = threading.Event()

    def reader_loop():
        nonlocal output
        while not stop_reader.is_set():
            ready, _, _ = select.select([master], [], [], 0.1)
            if not ready:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError:
                return
            if not chunk:
                return
            with output_lock:
                output += chunk.decode("utf-8", errors="replace")

    reader_thread = threading.Thread(target=reader_loop, daemon=True)
    reader_thread.start()

    try:
        def read_available():
            pass

        wait_for(lambda: socket_path.exists() or process.poll() is not None, 20, "socket startup", output, read_available)
        if process.poll() is not None:
            read_available()
            raise RuntimeError(f"Pi exited {process.returncode}: {output[-6000:]}")

        def rpc(request):
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
                client.settimeout(30)
                client.connect(str(socket_path))
                client.sendall(json.dumps(request, ensure_ascii=False).encode() + b"\n")
                data = b""
                while not data.endswith(b"\n"):
                    part = client.recv(65536)
                    if not part:
                        break
                    data += part
            response = json.loads(data)
            if not response.get("ok"):
                raise RuntimeError(f"RPC failed: {response}")
            return response

        def state():
            return rpc({"op": "state"})

        def write(data):
            os.write(master, data.encode() if isinstance(data, str) else data)

        def wait_idle(expected):
            return wait_for(lambda: state()["idle"] is expected, 15, f"idle={expected}", output, read_available)

        if busy:
            write("Запустить длинный ход\r")
            wait_idle(False)

        if "\n" in draft:
            first, second = draft.split("\n", 1)
            write(first)
            write("\\\r")
            write(second)
        else:
            write(draft)
        time.sleep(0.3)
        write("\x1b[D\x1b[D\x1b[D")
        time.sleep(0.2)
        before = state()
        assert before["text"] == draft, (before, draft)
        assert before["idle"] is (not busy), before
        assert 0 < before["cursorCol"] < len(before["lines"][before["cursorLine"]]), before

        injected = f"Внешнее сообщение {name}"
        send_result = rpc({"op": "send", "text": injected})
        assert send_result["deliverAs"] == "followUp", send_result
        assert send_result["expandPromptTemplates"] is False, send_result
        after = state()
        assert after["text"] == before["text"], (before, after)
        assert after["lines"] == before["lines"], (before, after)
        assert after["cursorLine"] == before["cursorLine"], (before, after)
        assert after["cursorCol"] == before["cursorCol"], (before, after)

        read_available()
        if process.poll() is not None:
            raise RuntimeError(f"Pi exited {process.returncode}: {output[-6000:]}")
        wait_idle(True)
        settled = state()
        assert settled["text"] == before["text"], (before, settled)
        assert settled["lines"] == before["lines"], (before, settled)
        assert settled["cursorLine"] == before["cursorLine"], (before, settled)
        assert settled["cursorCol"] == before["cursorCol"], (before, settled)
        assert settled["idle"] is True, settled

        events = [json.loads(line) for line in event_log.read_text().splitlines() if line]
        provider_starts = [event for event in events if event.get("event") == "provider_start"]
        assert len(provider_starts) == (2 if busy else 1), provider_starts
        sends = [event for event in events if event.get("event") == "send"]
        assert len(sends) == 1 and sends[0]["text"] == injected, sends
        assert sends[0]["deliverAs"] == "followUp" and sends[0]["expandPromptTemplates"] is False, sends
        transcript_entries = [
            json.loads(line)
            for path in sessions_dir.rglob("*.jsonl")
            for line in path.read_text().splitlines()
            if line
        ]
        conversation = [entry for entry in transcript_entries if entry.get("type") == "message"]
        injected_messages = []
        for entry in conversation:
            message = entry["message"]
            content = message.get("content")
            if message.get("role") != "user":
                continue
            if isinstance(content, str):
                texts = [content]
            else:
                texts = [part.get("text") for part in content if part.get("type") == "text"]
            if injected in texts:
                injected_messages.append(entry)
        assert len(injected_messages) == 1, injected_messages
        roles = [entry["message"]["role"] for entry in conversation if entry["message"]["role"] != "system"]
        assert roles == (["user", "assistant", "user", "assistant"] if busy else ["user", "assistant"]), roles

        result = {
            "scenario": name,
            "pi": PI_VERSION,
            "before": before,
            "after_send": after,
            "after_settled": settled,
            "provider_calls": len(provider_starts),
            "injected_message_id": injected_messages[0]["id"],
            "injected_transcript_count": len(injected_messages),
            "status": "PASS",
        }
        (scenario_dir / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
        return result
    finally:
        read_available()
        pty_log.write_text(output)
        if process.poll() is None:
            try:
                rpc({"op": "shutdown"})
            except Exception:
                pass
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.terminate()
                process.wait(timeout=15)
        stop_reader.set()
        reader_thread.join(timeout=2)
        pty_log.write_text(output)
        os.close(master)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", choices=[name for name, _, _ in SCENARIOS])
    args = parser.parse_args()
    selected = [scenario for scenario in SCENARIOS if args.only is None or scenario[0] == args.only]
    results = [run_scenario(*scenario) for scenario in selected]
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
