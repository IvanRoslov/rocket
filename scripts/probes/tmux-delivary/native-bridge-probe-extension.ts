import { appendFileSync, mkdirSync, unlinkSync } from "node:fs";
import { dirname } from "node:path";
import net from "node:net";
import { CustomEditor, type ExtensionAPI, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import {
	createAssistantMessageEventStream,
	type AssistantMessage,
	type Model,
	type SimpleStreamOptions,
	type TranscriptContext,
} from "@earendil-works/pi-ai";

// Research-only observer/provider for the production native bridge. It is
// loaded next to the real delivery-extension.ts, exposes stock editor state
// over a separate probe socket and can open extension UI prompts on request.
// The CustomEditor wrapper is read-only; no production hook uses this file.

const socketPath = process.env.PI_PROBE_SOCKET!;
const logPath = process.env.PI_PROBE_LOG!;
mkdirSync(dirname(logPath), { recursive: true });
let editor: CustomEditor | undefined;
let context: ExtensionContext | undefined;
let server: net.Server | undefined;
let editorInputCount = 0;
let editorLastInput: string | undefined;
let uiPromptStarts = 0;
let uiPromptEnds = 0;
let dialogOpen = false;
let dialogKind: string | undefined;
let customDone: ((result: unknown) => void) | undefined;

const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

const originalHandleInput = CustomEditor.prototype.handleInput;
CustomEditor.prototype.handleInput = function (data: string) {
	editor = this;
	editorInputCount += 1;
	editorLastInput = data;
	return originalHandleInput.call(this, data);
};

const state = () => {
	if (!editor) throw new Error("The stock editor has not received PTY input yet");
	return {
		text: editor.getText(),
		lines: [...editor.state.lines],
		cursorLine: editor.state.cursorLine,
		cursorCol: editor.state.cursorCol,
		idle: context?.isIdle() ?? false,
		model: context?.model ? `${context.model.provider}/${context.model.id}` : null,
		editorInputCount,
		editorLastInput,
		uiPromptStarts,
		uiPromptEnds,
		dialogOpen,
		dialogKind,
		sessionId: context?.sessionManager?.getSessionId() ?? null,
	};
};

const finishDialog = (answer: unknown) => {
	dialogOpen = false;
	dialogKind = undefined;
	customDone = undefined;
	log("dialog_resolved", { answer: answer ?? null });
};

const openDialog = (kind: string) => {
	if (!context) throw new Error("extension context is not ready");
	dialogOpen = true;
	dialogKind = kind;
	log("dialog_open", { kind });
	const ui = context.ui;
	const track = (promise: Promise<unknown>) => {
		promise.then(finishDialog, (error) => {
			log("dialog_error", { error: String(error) });
			finishDialog(undefined);
		});
	};
	if (kind === "select") track(ui.select("Probe modal", ["one", "two"]));
	else if (kind === "confirm") track(ui.confirm("Probe confirm", "message"));
	else if (kind === "input") track(ui.input("Probe input", "placeholder"));
	else if (kind === "editor") track(ui.editor("Probe editor", "prefill"));
	else if (kind === "custom") {
		track(ui.custom((_tui, _theme, _keybindings, done) => {
			customDone = done;
			return {
				render: () => ["Probe custom"],
				invalidate: () => {},
				handleInput: (data: string) => {
					if (data === "\x1b" || data === "x") done(undefined);
				},
			};
		}));
	} else {
		throw new Error(`unknown dialog kind ${kind}`);
	}
};

const startServer = () => {
	if (server) return;
	try {
		unlinkSync(socketPath);
	} catch {}
	server = net.createServer((connection) => {
		log("connection");
		let input = "";
		connection.on("data", (chunk) => {
			input += chunk.toString();
			const newline = input.indexOf("\n");
			if (newline === -1) return;
			const request = JSON.parse(input.slice(0, newline));
			input = input.slice(newline + 1);
			log("rpc", { op: request.op });
			try {
				if (request.op === "state") {
					const snapshot = state();
					log("state", snapshot);
					connection.end(`${JSON.stringify({ ok: true, ...snapshot })}\n`);
					return;
				}
				if (request.op === "openDialog") {
					openDialog(String(request.kind));
					connection.end(`${JSON.stringify({ ok: true, kind: request.kind })}\n`);
					return;
				}
				if (request.op === "closeCustom") {
					if (customDone) customDone(undefined);
					connection.end(`${JSON.stringify({ ok: true, closed: Boolean(customDone) })}\n`);
					return;
				}
				if (request.op === "compact") {
					log("compact_requested");
					context?.compact({});
					connection.end(`${JSON.stringify({ ok: true })}\n`);
					return;
				}
				if (request.op === "shutdown") {
					connection.end(`${JSON.stringify({ ok: true })}\n`);
					setImmediate(() => context?.shutdown());
					return;
				}
				connection.end(`${JSON.stringify({ ok: false, error: "unknown op" })}\n`);
			} catch (error) {
				connection.end(`${JSON.stringify({ ok: false, error: String(error) })}\n`);
			}
		});
	});
	server.listen(socketPath);
};

const streamSimple = (model: Model<any>, _context: TranscriptContext, options?: SimpleStreamOptions) => {
	const stream = createAssistantMessageEventStream();
	const output: AssistantMessage = {
		role: "assistant",
		content: [],
		api: model.api,
		provider: model.provider,
		model: model.id,
		usage: {
			input: 0,
			output: 0,
			cacheRead: 0,
			cacheWrite: 0,
			totalTokens: 0,
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
		},
		stopReason: "pending",
		timestamp: Date.now(),
	};
	log("provider_start", { model: model.id });
	stream.push({ type: "start", partial: output });
	stream.push({ type: "text_start", contentIndex: 0, partial: output });
	const delayMs = Number(process.env.PI_PROBE_DELAY_MS ?? "100");
	log("provider_scheduled", { delayMs });
	setTimeout(() => {
		log("provider_finish");
		const text = options?.signal?.aborted ? "" : "Deterministic local response";
		output.content = [{ type: "text", text }];
		stream.push({ type: "text_delta", contentIndex: 0, delta: text, partial: output });
		stream.push({ type: "text_end", contentIndex: 0, content: text, partial: output });
		output.stopReason = options?.signal?.aborted ? "aborted" : "stop";
		if (output.stopReason === "aborted") {
			stream.push({ type: "error", reason: "aborted", error: output });
		} else {
			stream.push({ type: "done", reason: "stop", message: output });
		}
		stream.end();
	}, delayMs);
	return stream;
};

let pi: ExtensionAPI;
export default function (api: ExtensionAPI) {
	pi = api;
	pi.registerProvider("probe", {
		baseUrl: "http://127.0.0.1",
		apiKey: "probe",
		api: "probe",
		models: [{
			id: "deterministic",
			name: "Deterministic local probe",
			reasoning: false,
			input: ["text"],
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
			contextWindow: 16000,
			maxTokens: 1024,
		}],
		streamSimple,
	});
	pi.on("session_start", (_event, ctx) => {
		context = ctx;
		startServer();
		log("session_start");
	});
	pi.on("session_shutdown", (event) => {
		server?.close();
		server = undefined;
		try {
			unlinkSync(socketPath);
		} catch {}
		log("session_shutdown", { reason: event.reason });
	});
	pi.on("ui_prompt_start", (event) => {
		uiPromptStarts += 1;
		log("ui_prompt_start", { kind: event.kind, title: event.title });
	});
	pi.on("ui_prompt_end", (event) => {
		uiPromptEnds += 1;
		log("ui_prompt_end", { kind: event.kind, title: event.title });
	});
	pi.on("session_before_compact", (event) => log("session_before_compact", { reason: event.reason }));
	pi.on("session_compact", (event) => log("session_compact", { reason: event.reason }));
	pi.on("session_compact_failed", (event) => log("session_compact_failed", { reason: event.reason }));
	pi.on("model_select", (event) => log("model_select", { model: `${event.model.provider}/${event.model.id}` }));
}
