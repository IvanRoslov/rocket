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

const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

const originalHandleInput = CustomEditor.prototype.handleInput;
CustomEditor.prototype.handleInput = function (data: string) {
	editor = this;
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
	log("provider_scheduled", { delayMs: Number(process.env.PI_PROBE_DELAY_MS ?? "100") });
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
	pi.on("session_shutdown", () => {
		server?.close();
		server = undefined;
		try {
			unlinkSync(socketPath);
		} catch {}
		log("session_shutdown");
	});
}
