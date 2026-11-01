import { appendFileSync, mkdirSync, unlinkSync } from "node:fs";
import { dirname } from "node:path";
import net from "node:net";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import {
	createAssistantMessageEventStream,
	type AssistantMessage,
	type Model,
	type SimpleStreamOptions,
	type TranscriptContext,
} from "@earendil-works/pi-ai";

// Research-only fixture for the ui_prompt_start ordering question. The flag here
// mimics a bridge guard; it is never a production guard.
const socketPath = process.env.PI_PROBE_SOCKET!;
const logPath = process.env.PI_PROBE_LOG!;
mkdirSync(dirname(logPath), { recursive: true });
let context: ExtensionContext | undefined;
let server: net.Server | undefined;
let modalFlag = false;
let promptOpen = false;
let resolvePrompt: (() => void) | undefined;

const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

const startServer = () => {
	if (server) return;
	try {
		unlinkSync(socketPath);
	} catch {}
	server = net.createServer((connection) => {
		let input = "";
		connection.on("data", (chunk) => {
			input += chunk.toString();
			const newline = input.indexOf("\n");
			if (newline === -1) return;
			const request = JSON.parse(input.slice(0, newline));
			input = input.slice(newline + 1);
			try {
				if (request.op === "state") {
					const snapshot = {
						flag: modalFlag,
						promptOpen,
						idle: context?.isIdle() ?? false,
						model: context?.model ? `${context.model.provider}/${context.model.id}` : null,
					};
					log("state", snapshot);
					connection.end(`${JSON.stringify({ ok: true, ...snapshot })}\n`);
					return;
				}
				if (request.op === "open") {
					promptOpen = true;
					const pending = context!.ui.select("Probe modal", ["one", "two"]);
					log("prompt_requested");
					pending.then((answer) => {
						promptOpen = false;
						log("prompt_resolved", { answer: answer ?? null });
						resolvePrompt?.();
					});
					connection.end(`${JSON.stringify({ ok: true })}\n`);
					return;
				}
				if (request.op === "send") {
					pi.sendUserMessage(request.text, {
						deliverAs: "followUp",
						expandPromptTemplates: false,
					});
					log("send", { text: request.text, flagAtSend: modalFlag, promptOpen });
					connection.end(`${JSON.stringify({ ok: true, flagAtSend: modalFlag, promptOpen })}\n`);
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
		const text = options?.signal?.aborted ? "" : "Timing probe response";
		output.content = [{ type: "text", text }];
		stream.push({ type: "text_delta", contentIndex: 0, delta: text, partial: output });
		stream.push({ type: "text_end", contentIndex: 0, content: text, partial: output });
		output.stopReason = options?.signal?.aborted ? "aborted" : "stop";
		stream.push({ type: "done", reason: "stop", message: output });
		log("provider_finish");
		stream.end();
	}, 150);
	return stream;
};

let pi: ExtensionAPI;
export default function (api: ExtensionAPI) {
	pi = api;
	pi.registerProvider("timing", {
		baseUrl: "http://127.0.0.1",
		apiKey: "timing",
		api: "timing",
		models: [{
			id: "deterministic",
			name: "Timing probe model",
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
	pi.on("ui_prompt_start", () => {
		modalFlag = true;
		log("flag_set");
	});
	pi.on("ui_prompt_end", () => {
		modalFlag = false;
		log("flag_cleared");
	});
}
