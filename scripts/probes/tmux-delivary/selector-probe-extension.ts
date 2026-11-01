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

const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

// Read-only instrumentation: the wrapper only records and delegates. It never
// changes editor text, cursor or behaviour, and it is never used by production code.
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
	};
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
				if (request.op === "send") {
					pi.sendUserMessage(request.text, {
						deliverAs: "followUp",
						expandPromptTemplates: false,
					});
					log("send", {
						text: request.text,
						deliverAs: "followUp",
						expandPromptTemplates: false,
					});
					connection.end(`${JSON.stringify({
						ok: true,
						deliverAs: "followUp",
						expandPromptTemplates: false,
					})}\n`);
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
	}, Number(process.env.PI_PROBE_DELAY_MS ?? "100"));
	return stream;
};

let pi: ExtensionAPI;
export default function (api: ExtensionAPI) {
	pi = api;
	const model = (id: string, name: string) => ({
		id,
		name,
		reasoning: false,
		input: ["text" as const],
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
		contextWindow: 16000,
		maxTokens: 1024,
	});
	pi.registerProvider("probe", {
		baseUrl: "http://127.0.0.1",
		apiKey: "probe",
		api: "probe",
		models: [
			model("deterministic", "Deterministic local probe"),
			model("secondary", "Secondary deterministic probe"),
		],
		streamSimple,
	});
	pi.on("session_start", (_event, ctx) => {
		context = ctx;
		startServer();
		log("session_start");
	});
	pi.on("session_shutdown", () => {
		server?.close();
		server = undefined;
		try {
			unlinkSync(socketPath);
		} catch {}
		log("session_shutdown");
	});
	pi.on("ui_prompt_start", (event) => {
		uiPromptStarts += 1;
		log("ui_prompt_start", { kind: event.kind, title: event.title });
	});
	pi.on("ui_prompt_end", (event) => {
		uiPromptEnds += 1;
		log("ui_prompt_end", { kind: event.kind, title: event.title });
	});
	pi.on("model_select", (event) => {
		log("model_select", {
			model: `${event.model.provider}/${event.model.id}`,
			previousModel: event.previousModel ? `${event.previousModel.provider}/${event.previousModel.id}` : null,
			source: event.source,
		});
	});
}
