#!/usr/bin/env python3
"""Bounded preflight: does a stock built-in selector stay safe when an external
followUp is delivered through the Pi 1.4.0 Extension API?

Research fixture only. Production launch stays on hold; this file must never be
used as a production guard or as a nativeinput contract.
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
EXTENSION = ROOT / "selector-probe-extension.ts"
SCENARIO = "selector-model"
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


def run_scenario():
    scenario_dir = ROOT / "results" / SCENARIO
    if scenario_dir.exists():
        shutil.rmtree(scenario_dir)
    cwd = scenario_dir / "cwd"
    agent_dir = scenario_dir / "agent"
    sessions_dir = scenario_dir / "sessions"
    for directory in (cwd, agent_dir, sessions_dir):
        directory.mkdir(parents=True, exist_ok=True)
    socket_path = Path("/tmp") / f"pi-selector-{os.getpid()}.sock"
    socket_path.unlink(missing_ok=True)
    event_log = scenario_dir / "events.jsonl"
    pty_log = scenario_dir / "pty.raw"
    env = os.environ.copy()
    # Keep the probe on the isolated deterministic provider: no inherited provider
    # credentials, so no provider auth/availability check can leave the machine.
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
        "PI_PROBE_DELAY_MS": "150",
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

        def wait_idle(expected):
            return wait_for(lambda: state()["idle"] is expected, 15, f"idle={expected}", output, read_available)

        draft = "Первая строка черновика\nВторая строка с курсором"
        first, second = draft.split("\n", 1)
        write(first)
        write("\\\r")
        write(second)
        time.sleep(0.3)
        write("\x1b[D\x1b[D\x1b[D")
        time.sleep(0.2)
        before = state()
        assert before["text"] == draft, (before, draft)
        assert before["idle"] is True, before
        assert before["model"] == "probe/deterministic", before
        assert 0 < before["cursorCol"] < len(before["lines"][before["cursorLine"]]), before

        # Open the stock built-in model selector (Ctrl+L, app.model.select).
        write("\x0c")
        wait_for(
            lambda: "Only showing models from configured providers" in plain(output),
            10,
            "model selector open",
            output,
            read_available,
        )
        selector_open_output = plain(output)
        assert "deterministic" in selector_open_output, selector_open_output[-3000:]

        # Highlight the non-current model so a synthesized Enter would visibly switch it.
        write("\x1b[B")
        time.sleep(0.2)
        open_state = state()
        assert open_state["text"] == draft, open_state

        injected = "Внешнее сообщение при открытом селекторе"
        send_result = rpc({"op": "send", "text": injected})
        assert send_result["deliverAs"] == "followUp", send_result
        assert send_result["expandPromptTemplates"] is False, send_result
        after_send = state()
        assert after_send["text"] == before["text"], (before, after_send)
        assert after_send["lines"] == before["lines"], (before, after_send)
        assert after_send["cursorLine"] == before["cursorLine"], (before, after_send)
        assert after_send["cursorCol"] == before["cursorCol"], (before, after_send)
        assert after_send["editorInputCount"] == open_state["editorInputCount"], (open_state, after_send)

        wait_idle(True)
        settled = state()
        assert settled["text"] == before["text"], (before, settled)
        assert settled["lines"] == before["lines"], (before, settled)
        assert settled["cursorLine"] == before["cursorLine"], (before, settled)
        assert settled["cursorCol"] == before["cursorCol"], (before, settled)
        assert settled["model"] == "probe/deterministic", settled
        assert settled["editorInputCount"] == open_state["editorInputCount"], (open_state, settled)

        # Selector usability after the injected message: keystrokes must still be
        # consumed by the selector, not by the stock editor.
        write("zz")
        wait_for(
            lambda: "No matching models" in plain(output),
            10,
            "selector search filtering",
            output,
            read_available,
        )
        after_typing = state()
        assert after_typing["editorInputCount"] == open_state["editorInputCount"], (open_state, after_typing)
        assert after_typing["text"] == before["text"], (before, after_typing)

        # Close the selector without selecting. The draft and cursor must survive.
        write("\x1b")
        time.sleep(0.5)
        after_escape = state()
        assert after_escape["text"] == before["text"], (before, after_escape)
        assert after_escape["lines"] == before["lines"], (before, after_escape)
        assert after_escape["cursorLine"] == before["cursorLine"], (before, after_escape)
        assert after_escape["cursorCol"] == before["cursorCol"], (before, after_escape)
        assert after_escape["model"] == "probe/deterministic", after_escape

        # Focus must be back on the editor: typing now inserts at the mid-line cursor.
        write("!")
        time.sleep(0.3)
        after_edit = state()
        cursor_line = before["cursorLine"]
        cursor_col = before["cursorCol"]
        expected_lines = list(before["lines"])
        expected_lines[cursor_line] = (
            expected_lines[cursor_line][:cursor_col] + "!" + expected_lines[cursor_line][cursor_col:]
        )
        assert after_edit["lines"] == expected_lines, (before, after_edit)
        assert after_edit["editorInputCount"] == after_escape["editorInputCount"] + 1, (after_escape, after_edit)

        events = [json.loads(line) for line in event_log.read_text().splitlines() if line]
        provider_starts = [event for event in events if event.get("event") == "provider_start"]
        assert len(provider_starts) == 1, provider_starts
        sends = [event for event in events if event.get("event") == "send"]
        assert len(sends) == 1 and sends[0]["text"] == injected, sends
        ui_prompts = [event for event in events if event.get("event") in ("ui_prompt_start", "ui_prompt_end")]
        assert ui_prompts == [], ui_prompts
        model_selects = [event for event in events if event.get("event") == "model_select"]
        assert model_selects == [], model_selects

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

        screen = plain(output)
        screen_evidence = []
        for marker, limit in (
            ("Only showing models", 320),
            ("Model catalogs refreshed.", 760),
            ("> zz", 180),
            ("Вторая строка с курсо!ром", 120),
        ):
            position = screen.find(marker)
            if position != -1:
                screen_evidence.append(compact(screen[position:position + limit], limit))
        result = {
            "scenario": SCENARIO,
            "pi": PI_VERSION,
            "selector": "stock built-in model selector (showModelSelector via showSelector, Ctrl+L)",
            "selector_marker": "Only showing models from configured providers",
            "draft": draft,
            "before": before,
            "selector_open": open_state,
            "after_send": after_send,
            "after_settled": settled,
            "after_search_typing": after_typing,
            "after_escape": after_escape,
            "after_editor_typing": after_edit,
            "expected_lines_after_typing": expected_lines,
            "provider_calls": len(provider_starts),
            "ui_prompt_events": len(ui_prompts),
            "model_select_events": len(model_selects),
            "injected_message_id": injected_messages[0]["id"],
            "injected_transcript_count": len(injected_messages),
            "screen_evidence": screen_evidence,
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
        socket_path.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser()
    parser.parse_args()
    result = run_scenario()
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
