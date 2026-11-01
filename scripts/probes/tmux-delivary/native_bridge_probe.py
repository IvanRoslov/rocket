#!/usr/bin/env python3
"""Live matrix for the production Pi native delivery bridge (task2D).

Each scenario launches stock Pi 1.4.0 in an isolated PTY with two explicit
extensions: the production embedded delivery-extension.ts and a research-only
observer/provider. The local deterministic provider is the only model; no
credentials, external providers or shared daemons are used.

Run from the worktree root:
    python3 scripts/probes/tmux-delivary/native_bridge_probe.py --all
"""
import argparse
import json
import os
import pty
import re
import secrets
import select
import shutil
import socket
import struct
import subprocess
import tempfile
import termios
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
OBSERVER = ROOT / "scripts" / "probes" / "tmux-delivary" / "native-bridge-probe-extension.ts"
PRODUCTION = ROOT / "internal" / "agent" / "pi" / "delivery-extension.ts"
SLOW = ROOT / "scripts" / "probes" / "tmux-delivary" / "slow-ui-listener-extension.ts"
INPUT_HANDLED = ROOT / "scripts" / "probes" / "tmux-delivary" / "input-handled-extension.ts"
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


def wait_until(predicate, timeout, description, pty_output, read_available):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        read_available()
        value = predicate()
        if value:
            return value
        time.sleep(0.05)
    read_available()
    raise TimeoutError(f"{description}; PTY tail: {plain(pty_output())[-4000:]}")


def probe_rpc(path, request):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(30)
        client.connect(str(path))
        client.sendall(json.dumps(request, ensure_ascii=False).encode() + b"\n")
        data = b""
        while not data.endswith(b"\n"):
            part = client.recv(65536)
            if not part:
                break
            data += part
    return json.loads(data)


def native_request(endpoint, session_id, instance_id, message_id, text, version=1, chunks=None, close_after=False):
    frame = json.dumps({
        "v": version,
        "session_id": session_id,
        "instance_id": instance_id,
        "message_id": message_id,
        "text": text,
    }, ensure_ascii=False).encode() + b"\n"
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(10)
        client.connect(str(endpoint))
        if chunks:
            for chunk in chunks:
                client.sendall(chunk)
                time.sleep(0.01)
            frame = b""
        else:
            client.sendall(frame)
            frame = b""
        if close_after:
            return None
        data = b""
        while not data.endswith(b"\n"):
            part = client.recv(65536)
            if not part:
                break
            data += part
    return json.loads(data)


class Session:
    def __init__(self, scenario, delay_ms=150, slow=False, extra_env=None,
                 native_expected=True, pi_session_id=None, input_handled_text=None):
        self.scenario = scenario
        self.dir = Path(tempfile.mkdtemp(prefix="rpi-live-", dir="/tmp"))
        os.chmod(self.dir, 0o700)
        self.cwd = self.dir / "cwd"
        self.agent_dir = self.dir / "agent"
        self.sessions_dir = self.dir / "sessions"
        for path in (self.cwd, self.agent_dir, self.sessions_dir):
            path.mkdir(parents=True, exist_ok=True)
        self.endpoint = self.dir / "s"
        self.probe_socket = self.dir / "p"
        self.events = self.dir / "events.jsonl"
        self.input_log = self.dir / "input-handled.jsonl"
        self.pty_raw = self.dir / "pty.raw"
        self.native_expected = native_expected
        self.input_handled_text = input_handled_text
        self.session_id = "live-" + scenario
        self.pi_session_id = pi_session_id or self.session_id
        self.instance_id = secrets.token_hex(8)
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
            "PI_CODING_AGENT_DIR": str(self.agent_dir),
            "PI_CODING_AGENT_SESSION_DIR": str(self.sessions_dir),
            "PI_TELEMETRY": "0",
            "PI_PROBE_SOCKET": str(self.probe_socket),
            "PI_PROBE_LOG": str(self.events),
            "PI_PROBE_DELAY_MS": str(delay_ms),
            "ROCKET_PI_NATIVE_SESSION_ID": self.session_id,
            "ROCKET_PI_NATIVE_INSTANCE_ID": self.instance_id,
            "ROCKET_PI_NATIVE_ENDPOINT": str(self.endpoint),
            "ROCKET_PI_NATIVE_PROTOCOL": "1",
            "TERM": "xterm-256color",
        })
        if slow:
            env["PI_TIMING_SLOW_LOG"] = str(self.dir / "slow-events.jsonl")
        if input_handled_text is not None:
            env["PI_INPUT_HANDLED_LOG"] = str(self.input_log)
            env["PI_INPUT_HANDLED_TEXT"] = input_handled_text
        if extra_env:
            env.update(extra_env)
        self.env = env
        command = [
            "pi", "--offline", "--no-extensions", "--no-mcp", "--no-skills",
            "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-tools",
            "--model", "probe/deterministic", "--session-dir", str(self.sessions_dir),
            "--session-id", self.pi_session_id,
        ]
        if slow:
            command.extend(["--extension", str(SLOW)])
        if input_handled_text is not None:
            command.extend(["--extension", str(INPUT_HANDLED)])
        command.extend(["--extension", str(OBSERVER), "--extension", str(PRODUCTION)])
        master, slave = pty.openpty()
        import fcntl
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        self.process = subprocess.Popen(
            command, cwd=self.cwd, env=env, stdin=slave, stdout=slave, stderr=slave,
            start_new_session=True, close_fds=True,
        )
        os.close(slave)
        self.master = master
        self.output = ""
        self.lock = threading.Lock()
        self.stop_reader = threading.Event()
        self.reader = threading.Thread(target=self._reader_loop, daemon=True)
        self.reader.start()
        self._wait_ready(20)

    def _reader_loop(self):
        while not self.stop_reader.is_set():
            ready, _, _ = select.select([self.master], [], [], 0.1)
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

    def read_available(self):
        pass

    def pty_text(self):
        with self.lock:
            return self.output

    def _wait_ready(self, timeout):
        def ready():
            if self.process.poll() is not None:
                raise RuntimeError(f"Pi exited {self.process.returncode}: {plain(self.pty_text())[-6000:]}")
            if not self.probe_socket.exists():
                return False
            return self.endpoint.exists() if self.native_expected else True

        wait_until(
            ready, timeout,
            "native and probe sockets" if self.native_expected else "probe socket",
            self.pty_text, self.read_available,
        )
        # The read-only observer learns the stock editor from its first input.
        # A left arrow at column 0 changes neither the draft nor the cursor.
        self.write("\x1b[D")
        time.sleep(0.2)
        wait_until(lambda: self.state().get("idle") is True, 15, "initial idle", self.pty_text, self.read_available)
        # The native target must be the actual Pi history, not only an env
        # string: bind the fixture to the session the runtime reports.
        observed = self.state().get("sessionId")
        if observed != self.pi_session_id:
            raise RuntimeError(
                f"Pi history {observed!r} does not match the fixture target {self.pi_session_id!r}"
            )

    def write(self, data):
        os.write(self.master, data.encode() if isinstance(data, str) else data)

    def state(self):
        return probe_rpc(self.probe_socket, {"op": "state"})

    def open_dialog(self, kind):
        return probe_rpc(self.probe_socket, {"op": "openDialog", "kind": kind})

    def close_custom(self):
        return probe_rpc(self.probe_socket, {"op": "closeCustom"})

    def compact(self):
        return probe_rpc(self.probe_socket, {"op": "compact"})

    def event_names(self):
        if not self.events.exists():
            return []
        return [json.loads(line)["event"] for line in self.events.read_text().splitlines() if line]

    def events_list(self):
        if not self.events.exists():
            return []
        return [json.loads(line) for line in self.events.read_text().splitlines() if line]

    def input_events(self):
        if not self.input_log.exists():
            return []
        return [json.loads(line) for line in self.input_log.read_text().splitlines() if line]

    def wait_event_count(self, name, count, timeout=15):
        return wait_until(
            lambda: self.event_names().count(name) >= count,
            timeout,
            f"{name} x{count}",
            self.pty_text,
            self.read_available,
        )

    def wait_idle(self, expected=True, timeout=20):
        return wait_until(
            lambda: self.state()["idle"] is expected,
            timeout,
            f"idle={expected}",
            self.pty_text,
            self.read_available,
        )

    def native(self, message_id, text, **kwargs):
        return native_request(self.endpoint, self.session_id, self.instance_id, message_id, text, **kwargs)

    def transcript_messages(self):
        messages = []
        for path in sorted(self.sessions_dir.rglob("*.jsonl")):
            for line in path.read_text().splitlines():
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if entry.get("type") != "message":
                    continue
                message = entry.get("message") or {}
                role = message.get("role")
                content = message.get("content")
                if isinstance(content, str):
                    texts = [content]
                else:
                    texts = [part.get("text") for part in content or [] if part.get("type") == "text"]
                messages.append({"role": role, "texts": texts, "entry": entry})
        return messages

    def user_texts(self):
        return [text for msg in self.transcript_messages() if msg["role"] == "user" for text in msg["texts"]]

    def provider_calls(self):
        return self.event_names().count("provider_start")

    def screen_evidence(self, markers):
        screen = plain(self.pty_text())
        evidence = []
        for marker, limit in markers:
            position = screen.find(marker)
            if position != -1:
                evidence.append(compact(screen[position:position + limit], limit))
        return evidence

    def close(self):
        try:
            probe_rpc(self.probe_socket, {"op": "shutdown"})
        except Exception:
            pass
        try:
            self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=10)
        self.stop_reader.set()
        self.reader.join(timeout=2)
        with self.lock:
            self.pty_raw.write_text(self.output)
        try:
            os.close(self.master)
        except OSError:
            pass

    def cleanup(self):
        self.close()
        shutil.rmtree(self.dir, ignore_errors=True)

    def base_result(self):
        return {
            "scenario": self.scenario,
            "pi": PI_VERSION,
            "session_id": self.session_id,
            "pi_session_id": self.pi_session_id,
            "instance_id": self.instance_id,
            "endpoint_kind": "short /tmp UDS",
            "extensions": [OBSERVER.name, PRODUCTION.name],
        }


def set_draft(session, text, multiline=False, cursor_left=3):
    if multiline:
        first, second = text.split("\n", 1)
        session.write(first)
        session.write("\\\r")
        session.write(second)
    else:
        session.write(text)
    time.sleep(0.3)
    if cursor_left:
        session.write("\x1b[D" * cursor_left)
    time.sleep(0.2)
    return session.state()


def check_draft(session, before, label, result):
    after = session.state()
    result[f"{label}_state"] = after
    result[f"{label}_preserved"] = (
        after["text"] == before["text"]
        and after["lines"] == before["lines"]
        and after["cursorLine"] == before["cursorLine"]
        and after["cursorCol"] == before["cursorCol"]
    )
    return after


def scenario_idle(name, text, multiline=False):
    s = Session(name)
    try:
        result = s.base_result()
        if text:
            before = set_draft(s, text, multiline=multiline)
            result["before"] = before
        injected = f"Внешнее сообщение {name}"
        result["response"] = s.native(101, injected)
        s.wait_idle(True)
        after = s.state()
        if text:
            result["after_settled"] = after
            result["draft_preserved"] = (
                after["text"] == before["text"]
                and after["lines"] == before["lines"]
                and after["cursorLine"] == before["cursorLine"]
                and after["cursorCol"] == before["cursorCol"]
            )
        else:
            result["after_settled"] = after
            result["draft_empty"] = after["text"] == ""
        users = s.user_texts()
        result["injected_count"] = users.count(injected)
        result["user_texts"] = users
        result["provider_calls"] = s.provider_calls()
        result["status"] = "PASS" if result["injected_count"] == 1 and result.get("draft_preserved", True) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_busy(name, text, multiline=False):
    s = Session(name, delay_ms=2500)
    try:
        result = s.base_result()
        s.write("run")
        s.write("\r")
        s.wait_event_count("provider_start", 1)
        before = set_draft(s, text, multiline=multiline)
        result["before"] = before
        result["busy_idle"] = s.state()["idle"]
        injected = f"Внешнее сообщение {name}"
        result["response"] = s.native(201, injected)
        after_send = s.state()
        result["after_send"] = after_send
        s.wait_idle(True, timeout=25)
        after = s.state()
        result["after_settled"] = after
        result["draft_preserved"] = (
            after["text"] == before["text"]
            and after["lines"] == before["lines"]
            and after["cursorLine"] == before["cursorLine"]
            and after["cursorCol"] == before["cursorCol"]
        )
        users = s.user_texts()
        result["injected_count"] = users.count(injected)
        result["user_texts"] = users
        result["provider_calls"] = s.provider_calls()
        result["user_role_order"] = [m["role"] for m in s.transcript_messages()]
        result["status"] = "PASS" if result["injected_count"] == 1 and result["draft_preserved"] and result["provider_calls"] == 2 else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_selector():
    s = Session("selector")
    try:
        result = s.base_result()
        before = set_draft(s, "Первая строка черновика\nВторая строка с курсором", multiline=True, cursor_left=3)
        result["before"] = before
        s.write("\x0c")
        wait_until(lambda: "Only showing models" in plain(s.pty_text()), 10, "model selector", s.pty_text, s.read_available)
        s.write("\x1b[B")
        time.sleep(0.2)
        injected = "Внешнее сообщение при открытом селекторе"
        result["response"] = s.native(301, injected)
        after_send = s.state()
        result["after_send"] = after_send
        s.wait_idle(True)
        result["selector_still_open"] = "Only showing models" in plain(s.pty_text())
        s.write("zz")
        wait_until(lambda: "No matching models" in plain(s.pty_text()), 10, "selector search", s.pty_text, s.read_available)
        after_search = s.state()
        result["after_search"] = after_search
        s.write("\x1b")
        time.sleep(0.5)
        after_escape = s.state()
        result["after_escape"] = after_escape
        s.write("!")
        time.sleep(0.3)
        after_edit = s.state()
        result["after_editor_typing"] = after_edit
        expected = list(before["lines"])
        line = before["cursorLine"]
        col = before["cursorCol"]
        expected[line] = expected[line][:col] + "!" + expected[line][col:]
        result["expected_lines_after_typing"] = expected
        result["draft_preserved"] = all(
            after_send[k] == before[k] for k in ("text", "lines", "cursorLine", "cursorCol")
        ) and all(after_escape[k] == before[k] for k in ("text", "lines", "cursorLine", "cursorCol"))
        result["injected_count"] = s.user_texts().count(injected)
        result["ui_prompt_events"] = s.event_names().count("ui_prompt_start") + s.event_names().count("ui_prompt_end")
        result["model_selects"] = s.event_names().count("model_select")
        result["screen_evidence"] = s.screen_evidence([
            ("Only showing models", 320),
            ("> zz", 180),
            ("Вторая строка с курсо!ром", 120),
        ])
        result["status"] = "PASS" if (
            result["draft_preserved"]
            and after_edit["lines"] == expected
            and result["injected_count"] == 1
            and result["model_selects"] == 0
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_modal_guard():
    s = Session("modal-guard")
    try:
        result = s.base_result()
        kinds = {}
        for index, kind in enumerate(("select", "confirm", "input", "editor", "custom"), start=1):
            s.open_dialog(kind)
            wait_until(lambda: s.state()["dialogOpen"], 5, f"{kind} open", s.pty_text, s.read_available)
            refused = s.native(400 + index, f"во время {kind}")
            kinds[kind] = {"refused": refused, "transcript_before": len(s.user_texts())}
            if kind == "custom":
                s.close_custom()
            else:
                s.write("\x1b")
            wait_until(lambda: not s.state()["dialogOpen"], 10, f"{kind} closed", s.pty_text, s.read_available)
            accepted = s.native(410 + index, f"после {kind}")
            s.wait_idle(True)
            kinds[kind].update({"accepted": accepted, "transcript_after": len(s.user_texts())})
        result["kinds"] = kinds
        result["injected_texts"] = s.user_texts()
        result["provider_calls"] = s.provider_calls()
        result["status"] = "PASS" if all(
            v["refused"]["outcome"] == "not_sent" and v["accepted"]["outcome"] == "accepted"
            and v["transcript_before"] == v["transcript_after"] - 1
            for v in kinds.values()
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_delayed_modal():
    s = Session("delayed-modal", slow=True, extra_env={"PI_TIMING_SLOW_MS": "1200"})
    try:
        result = s.base_result()
        s.open_dialog("select")
        wait_until(lambda: s.state()["dialogOpen"], 5, "dialog open", s.pty_text, s.read_available)
        # The production guard is delayed by the earlier async listener; the
        # accepted spec exception allows the turn to start while the dialog is open.
        injected = "доставка при задержанном guard"
        response = s.native(501, injected)
        result["response"] = response
        s.wait_idle(True)
        result["injected_count"] = s.user_texts().count(injected)
        result["dialog_still_open"] = s.state()["dialogOpen"]
        result["ui_prompt_starts"] = s.event_names().count("ui_prompt_start")
        result["status"] = "PASS" if response["outcome"] == "accepted" and result["injected_count"] == 1 else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_target_and_frames():
    s = Session("target-frames")
    try:
        result = s.base_result()
        before_users = len(s.user_texts())
        wrong = [
            native_request(s.endpoint, "other", s.instance_id, 601, "wrong session"),
            native_request(s.endpoint, s.session_id, "other", 602, "wrong instance"),
            native_request(s.endpoint, s.session_id, s.instance_id, 603, "wrong version", version=2),
            native_request(s.endpoint, s.session_id, s.instance_id, 0, "wrong id"),
        ]
        result["wrong_targets"] = wrong
        result["wrong_target_transcript_delta"] = len(s.user_texts()) - before_users

        oversized = b"a" * (1 << 20) + b"\n"
        result["oversized"] = native_request(
            s.endpoint, s.session_id, s.instance_id, 604, "", chunks=[oversized]
        )
        result["oversized_transcript_delta"] = len(s.user_texts()) - before_users

        text = "Привет, мир — UTF-8"
        frame = json.dumps({
            "v": 1, "session_id": s.session_id, "instance_id": s.instance_id, "message_id": 605, "text": text,
        }, ensure_ascii=False).encode() + b"\n"
        split = frame.index("Привет".encode()) + 3
        result["split_utf8"] = native_request(
            s.endpoint, s.session_id, s.instance_id, 605, text,
            chunks=[frame[:split], frame[split:]],
        )
        s.wait_idle(True)
        result["split_utf8_count"] = s.user_texts().count(text)

        equal = "Одинаковый текст"
        result["equal_1"] = s.native(606, equal)
        result["equal_2"] = s.native(607, equal)
        result["duplicate"] = s.native(608, "Повторный ID")
        result["duplicate_repeat"] = s.native(608, "Повторный ID")
        s.wait_idle(True)
        users = s.user_texts()
        result["equal_count"] = users.count(equal)
        result["duplicate_count"] = users.count("Повторный ID")
        result["provider_calls"] = s.provider_calls()
        result["status"] = "PASS" if (
            all(item["outcome"] == "not_sent" for item in wrong)
            and result["wrong_target_transcript_delta"] == 0
            and result["oversized"]["outcome"] == "not_sent"
            and result["oversized_transcript_delta"] == 0
            and result["split_utf8"]["outcome"] == "accepted"
            and result["split_utf8_count"] == 1
            and result["equal_count"] == 2
            and result["duplicate_count"] == 1
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_escape_dequeue():
    s = Session("escape-dequeue", delay_ms=2500)
    try:
        result = s.base_result()
        s.write("run")
        s.write("\r")
        s.wait_event_count("provider_start", 1)
        before = set_draft(s, "Черновик до Escape", cursor_left=0)
        result["before"] = before
        injected = "Очередное сообщение перед Escape"
        result["response"] = s.native(701, injected)
        time.sleep(0.3)
        s.write("\x1b")
        time.sleep(1.0)
        after = s.state()
        result["after_escape"] = after
        result["editor_contains_queued"] = injected in after["text"]
        result["cursor_at_end"] = after["cursorLine"] == len(after["lines"]) - 1 and after["cursorCol"] == len(after["lines"][-1])
        result["injected_count"] = s.user_texts().count(injected)
        result["status"] = "PASS" if result["editor_contains_queued"] and result["cursor_at_end"] and result["injected_count"] == 0 else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_reload():
    s = Session("reload")
    try:
        result = s.base_result()
        injected = "До reload"
        result["first"] = s.native(801, injected)
        s.wait_idle(True)
        first_provider = s.provider_calls()
        s.write("/reload")
        s.write("\r")
        wait_until(lambda: s.event_names().count("session_shutdown") >= 1, 10, "reload shutdown", s.pty_text, s.read_available)
        wait_until(lambda: s.endpoint.exists(), 10, "reload endpoint", s.pty_text, s.read_available)
        time.sleep(0.5)
        # The reloaded observer learns the new stock editor from its first input.
        s.write("\x1b[D")
        time.sleep(0.2)
        s.wait_idle(True)
        result["second"] = s.native(801, injected)
        s.wait_idle(True)
        result["provider_calls_before"] = first_provider
        result["provider_calls_after"] = s.provider_calls()
        result["injected_count"] = s.user_texts().count(injected)
        result["status"] = "PASS" if (
            result["first"]["outcome"] == "accepted"
            and result["second"]["outcome"] == "accepted"
            and result["injected_count"] == 1
            and result["provider_calls_after"] == result["provider_calls_before"]
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_foreign_history():
    # The stale ROCKET env names history A while Pi actually runs history B,
    # the repro from the review: after a switch+reload the old bridge must
    # never open A's endpoint inside B.
    s = Session("foreign-history", native_expected=False, pi_session_id="live-foreign-history-b")
    try:
        result = s.base_result()
        result["observed_pi_session"] = s.state().get("sessionId")
        time.sleep(0.5)
        result["endpoint_exists"] = s.endpoint.exists()
        try:
            s.native(901, "чужая история")
            result["native_outcome"] = "connected"
        except OSError as error:
            result["native_error"] = str(error)
        time.sleep(0.2)
        result["injected_count"] = len(s.user_texts())
        result["provider_calls"] = s.provider_calls()
        result["status"] = "PASS" if (
            result["observed_pi_session"] == "live-foreign-history-b"
            and not result["endpoint_exists"]
            and result["injected_count"] == 0
            and result["provider_calls"] == 0
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


def scenario_handled_input():
    # Q5 witness through the supported public Extension API: the first
    # extension-sourced input identical to the witness text is answered with
    # {action:"handled"} (no user record), the second identical input
    # continues and is recorded. Both bridge responses stay accepted.
    witness = "Q5 одинаковый witness"
    s = Session("handled-input", input_handled_text=witness)
    try:
        result = s.base_result()
        result["extensions"] = [INPUT_HANDLED.name, OBSERVER.name, PRODUCTION.name]
        result["witness_text"] = witness
        result["first"] = s.native(1001, witness)
        result["second"] = s.native(1002, witness)
        wait_until(lambda: s.user_texts().count(witness) >= 1, 10, "witness user record", s.pty_text, s.read_available)
        s.wait_idle(True)
        rows = s.input_events()
        result["input_events"] = rows
        result["handled_events"] = [row for row in rows if row["event"] == "handled"]
        result["continued_events"] = [row for row in rows if row["event"] == "continued"]
        result["input_sources"] = [row.get("source") for row in rows if row["event"] == "input"]
        result["user_texts"] = s.user_texts()
        result["matching_records"] = s.user_texts().count(witness)
        result["provider_calls"] = s.provider_calls()
        result["status"] = "PASS" if (
            result["first"]["outcome"] == "accepted"
            and result["second"]["outcome"] == "accepted"
            and len(result["handled_events"]) == 1
            and len(result["continued_events"]) == 1
            and result["matching_records"] == 1
            and result["input_sources"] == ["extension", "extension"]
        ) else "FAIL"
        return result
    finally:
        s.cleanup()


SCENARIOS = {
    "idle-single": lambda: scenario_idle("idle-single", "Русский однострочный черновик", multiline=False),
    "idle-multiline": lambda: scenario_idle("idle-multiline", "Первая строка\nВторая строка с курсором", multiline=True),
    "empty": lambda: scenario_idle("empty", "", multiline=False),
    "long-pointer": lambda: scenario_idle(
        "long-pointer",
        "[from orch] [large message] Full text written to .rocket/inbox/msg-999.md — read that file now. " + "x" * 600,
        multiline=False,
    ),
    "busy-single": lambda: scenario_busy("busy-single", "Русский черновик во время хода", multiline=False),
    "busy-multiline": lambda: scenario_busy("busy-multiline", "Первая строка\nВторая строка во время хода", multiline=True),
    "selector": scenario_selector,
    "modal-guard": scenario_modal_guard,
    "delayed-modal": scenario_delayed_modal,
    "target-frames": scenario_target_and_frames,
    "escape-dequeue": scenario_escape_dequeue,
    "reload": scenario_reload,
    "foreign-history": scenario_foreign_history,
    "handled-input": scenario_handled_input,
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--scenario", action="append", choices=sorted(SCENARIOS))
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--out", default=str(ROOT / "scripts" / "probes" / "tmux-delivary" / "results" / "native-bridge"))
    args = parser.parse_args()
    names = sorted(SCENARIOS) if args.all else args.scenario or ["idle-single"]
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    failures = []
    for name in names:
        print(f"=== {name} ===", flush=True)
        try:
            result = SCENARIOS[name]()
        except Exception as error:
            result = {"scenario": name, "pi": PI_VERSION, "status": "ERROR", "error": str(error)}
        directory = out / name
        directory.mkdir(parents=True, exist_ok=True)
        (directory / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
        print(json.dumps({"scenario": name, "status": result.get("status"), "error": result.get("error")}, ensure_ascii=False))
        if result.get("status") != "PASS":
            failures.append(name)
    if failures:
        print("FAILED:", ", ".join(failures))
        raise SystemExit(1)
    print("ALL PASS")


if __name__ == "__main__":
    main()
