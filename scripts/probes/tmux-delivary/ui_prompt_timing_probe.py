#!/usr/bin/env python3
"""Bounded preflight: is the ui_prompt_start guard delayed by listener ordering?

Research fixture only. Two modes:

* default (reproducer): two isolated extensions are loaded on purpose:
  1. slow-ui-listener-extension.ts (first) with an async ui_prompt_start handler;
  2. ui-timing-probe-extension.ts (second) which sets a bridge-like flag.
* --control: only the flag extension is loaded, i.e. a synchronous handler.

The claim under test is NOT "queueMicrotask is a race by itself": a socket
callback is a macrotask and cannot run between queueMicrotask() and microtask
drain. The claim is that ExtensionRunner.emit awaits handlers sequentially in
extension load order, so an earlier async handler delays the guard flag while
the modal is already open.
"""
import argparse
import fcntl
import json
import os
import pty
import re
import select
import shutil
import socket
import struct
import subprocess
import termios
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent
SLOW_EXTENSION = ROOT / "slow-ui-listener-extension.ts"
PROBE_EXTENSION = ROOT / "ui-timing-probe-extension.ts"
SCENARIO = "ui-prompt-timing"
CONTROL_SCENARIO = "ui-prompt-timing-control"
SLOW_MS = 1200
PI_VERSION = subprocess.check_output(["pi", "--version"], text=True).strip()
if PI_VERSION != "1.4.0":
    raise RuntimeError(f"Pi 1.4.0 is required, found {PI_VERSION}")

ANSI_RE = re.compile(
    r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)"
    r"|\x1b\[[0-9;?<=>]*[ -/]*[@-~]"
    r"|\x1b[()][A-Za-z0-9]"
    r"|\x1b[>=NOM78]"
)


def plain(text):
    return ANSI_RE.sub("", text).replace("\r", "")


def compact(text, limit):
    return " ".join(text.split())[:limit]


def wait_for(predicate, timeout, description, output, read_available):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        read_available()
        value = predicate()
        if value:
            return value
        time.sleep(0.05)
    read_available()
    raise TimeoutError(f"{description}; PTY tail: {plain(output)[-4000:]}")


def read_events(path):
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text().splitlines() if line]


def first_event(events, name):
    return next((event for event in events if event.get("event") == name), None)


def read_conversation(sessions_dir):
    entries = [
        json.loads(line)
        for path in sessions_dir.rglob("*.jsonl")
        for line in path.read_text().splitlines()
        if line
    ]
    return [entry for entry in entries if entry.get("type") == "message"]


def run_scenario(control):
    scenario = CONTROL_SCENARIO if control else SCENARIO
    scenario_dir = ROOT / "results" / scenario
    if scenario_dir.exists():
        shutil.rmtree(scenario_dir)
    cwd = scenario_dir / "cwd"
    agent_dir = scenario_dir / "agent"
    sessions_dir = scenario_dir / "sessions"
    for directory in (cwd, agent_dir, sessions_dir):
        directory.mkdir(parents=True, exist_ok=True)
    socket_path = Path("/tmp") / f"pi-timing-{os.getpid()}.sock"
    socket_path.unlink(missing_ok=True)
    event_log = scenario_dir / "events.jsonl"
    slow_log = scenario_dir / "slow-events.jsonl"
    pty_log = scenario_dir / "pty.raw"
    env = os.environ.copy()
    credential = re.compile(r"(_API_KEY|_AUTH_TOKEN|_OAUTH_TOKEN|_ACCESS_TOKEN)$", re.IGNORECASE)
    provider_prefix = re.compile(
        r"^(DEEPSEEK|OPENAI|ANTHROPIC|GOOGLE|GEMINI|OPENROUTER|XAI|GROQ|MISTRAL|AZURE|AWS|COPILOT|ZAI|KIMI|MOONSHOT)",
        re.IGNORECASE,
    )
    for key in [key for key in env if credential.search(key) or provider_prefix.match(key)]:
        del env[key]
    env.update({
        "PI_OFFLINE": "1",
        "PI_SKIP_VERSION_CHECK": "1",
        "PI_CODING_AGENT_DIR": str(agent_dir),
        "PI_CODING_AGENT_SESSION_DIR": str(sessions_dir),
        "PI_TELEMETRY": "0",
        "PI_PROBE_SOCKET": str(socket_path),
        "PI_PROBE_LOG": str(event_log),
        "PI_TIMING_SLOW_LOG": str(slow_log),
        "PI_TIMING_SLOW_MS": str(SLOW_MS),
        "TERM": "xterm-256color",
    })
    extensions = [PROBE_EXTENSION] if control else [SLOW_EXTENSION, PROBE_EXTENSION]
    command = [
        "pi", "--offline", "--no-extensions", "--no-mcp", "--no-skills",
        "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-tools",
        "--model", "timing/deterministic", "--session-dir", str(sessions_dir),
    ]
    for extension in extensions:
        command.extend(["--extension", str(extension)])
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
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
            raise RuntimeError(f"Pi exited {process.returncode}: {plain(output)[-6000:]}")

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

        wait_for(lambda: state()["idle"] is True, 15, "initial idle", output, read_available)

        opened_at = time.time() * 1000
        open_result = rpc({"op": "open"})
        assert open_result["ok"] is True, open_result
        wait_for(
            lambda: "Probe modal" in plain(output),
            10,
            "extension modal visible",
            output,
            read_available,
        )

        during_window = state()
        assert during_window["promptOpen"] is True, during_window
        assert during_window["idle"] is True, during_window
        prompt_requested = first_event(read_events(event_log), "prompt_requested")
        assert prompt_requested, read_events(event_log)

        screen = plain(output)
        screen_evidence = []
        for marker, limit in (("Probe modal", 260), ("во время открытого", 260)):
            position = screen.find(marker)
            if position != -1:
                screen_evidence.append(compact(screen[position:position + limit], limit))
        result = {
            "scenario": scenario,
            "pi": PI_VERSION,
            "mode": "control-synchronous-handler" if control else "reproducer-async-earlier-handler",
            "slow_handler_ms": None if control else SLOW_MS,
            "extension_order": [extension.name for extension in extensions],
            "state_during_window": during_window,
            "screen_evidence": screen_evidence,
        }

        if control:
            assert during_window["flag"] is True, during_window
            flag_set = first_event(read_events(event_log), "flag_set")
            assert flag_set, read_events(event_log)
            assert flag_set["time"] - prompt_requested["time"] <= 100, (prompt_requested, flag_set)
            result["timeline"] = {
                "opened_at": opened_at,
                "prompt_requested_at": prompt_requested["time"],
                "flag_set_at": flag_set["time"],
            }
            result["guard_delay_from_prompt_ms"] = flag_set["time"] - prompt_requested["time"]
            write("\x1b")
            wait_for(
                lambda: any(event.get("event") == "prompt_resolved" for event in read_events(event_log)),
                10,
                "modal closed",
                output,
                read_available,
            )
            wait_for(
                lambda: any(event.get("event") == "flag_cleared" for event in read_events(event_log)),
                5,
                "flag clear",
                output,
                read_available,
            )
            final = state()
            assert final["flag"] is False and final["promptOpen"] is False, final
            conversation = read_conversation(sessions_dir)
            assert conversation == [], conversation
            result["state_final"] = final
            result["transcript_messages"] = 0
            result["status"] = "PASS"
            (scenario_dir / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
            return result

        assert during_window["flag"] is False, during_window

        injected = "Сообщение во время открытого модального диалога"
        send_result = rpc({"op": "send", "text": injected})
        assert send_result["flagAtSend"] is False, send_result
        assert send_result["promptOpen"] is True, send_result
        send_at = time.time() * 1000

        wait_for(lambda: state()["idle"] is True, 15, "run finished", output, read_available)
        after_run = state()
        assert after_run["promptOpen"] is True, after_run
        assert after_run["flag"] is False, after_run

        wait_for(
            lambda: any(event.get("event") == "flag_set" for event in read_events(event_log)),
            SLOW_MS / 1000 + 5,
            "delayed guard flag",
            output,
            read_available,
        )
        flag_late = state()
        assert flag_late["flag"] is True, flag_late
        assert flag_late["promptOpen"] is True, flag_late

        events = read_events(event_log)
        slow_events = read_events(slow_log)
        slow_start_begin = first_event(slow_events, "slow_start_begin")
        slow_start_end = first_event(slow_events, "slow_start_end")
        flag_set = first_event(events, "flag_set")
        send_event = first_event(events, "send")
        provider_start = first_event(events, "provider_start")
        assert slow_start_begin and slow_start_end and flag_set and send_event and provider_start, (events, slow_events)
        assert send_event["time"] < slow_start_end["time"], (send_event, slow_start_end)
        assert slow_start_end["time"] <= flag_set["time"], (slow_start_end, flag_set)
        assert provider_start["time"] < flag_set["time"], (provider_start, flag_set)

        write("\x1b")
        wait_for(
            lambda: any(event.get("event") == "prompt_resolved" for event in read_events(event_log)),
            10,
            "modal closed",
            output,
            read_available,
        )
        wait_for(
            lambda: any(event.get("event") == "flag_cleared" for event in read_events(event_log)),
            SLOW_MS / 1000 + 5,
            "delayed flag clear",
            output,
            read_available,
        )
        events = read_events(event_log)
        slow_events = read_events(slow_log)
        slow_end_end = first_event(slow_events, "slow_end_end")
        flag_cleared = first_event(events, "flag_cleared")
        assert slow_end_end and flag_cleared, (events, slow_events)
        assert slow_end_end["time"] <= flag_cleared["time"], (slow_end_end, flag_cleared)
        final = state()
        assert final["flag"] is False and final["promptOpen"] is False, final

        conversation = read_conversation(sessions_dir)
        injected_messages = []
        for entry in conversation:
            message = entry["message"]
            if message.get("role") != "user":
                continue
            content = message.get("content")
            texts = [content] if isinstance(content, str) else [
                part.get("text") for part in content if part.get("type") == "text"
            ]
            if injected in texts:
                injected_messages.append(entry)
        assert len(injected_messages) == 1, injected_messages
        roles = [entry["message"]["role"] for entry in conversation if entry["message"]["role"] != "system"]
        assert roles == ["user", "assistant"], roles

        result.update({
            "injected_message_id": injected_messages[0]["id"],
            "injected_transcript_count": len(injected_messages),
            "timeline": {
                "opened_at": opened_at,
                "send_at": send_at,
                "prompt_requested_at": prompt_requested["time"],
                "provider_start_at": provider_start["time"],
                "slow_start_begin_at": slow_start_begin["time"],
                "slow_start_end_at": slow_start_end["time"],
                "flag_set_at": flag_set["time"],
                "slow_end_end_at": slow_end_end["time"],
                "flag_cleared_at": flag_cleared["time"],
            },
            "send_result": send_result,
            "state_after_run": after_run,
            "state_flag_late": flag_late,
            "state_final": final,
            "guard_delay_from_prompt_ms": flag_set["time"] - slow_start_begin["time"],
            "status": "PASS",
        })
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
        socket_path.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--control", action="store_true", help="run only the synchronous-handler control")
    args = parser.parse_args()
    result = run_scenario(args.control)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
