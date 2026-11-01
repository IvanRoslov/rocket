import { appendFileSync, chmodSync, mkdirSync, readFileSync, unlinkSync } from "node:fs";
import net from "node:net";
import { dirname, join } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// Rocket's native Pi delivery bridge (pi-uds-v1). It is an isolated embedded
// extension loaded with an explicit --extension flag only for supported Pi
// 1.4+ launches. It never touches the editor: requests are submitted through
// the public Extension API with followUp and without prompt expansion.

export const PROTOCOL_VERSION = 1;
export const MAX_FRAME_BYTES = 1 << 20; // including the newline

export const NATIVE_ENV = {
	session: "ROCKET_PI_NATIVE_SESSION_ID",
	instance: "ROCKET_PI_NATIVE_INSTANCE_ID",
	endpoint: "ROCKET_PI_NATIVE_ENDPOINT",
	protocol: "ROCKET_PI_NATIVE_PROTOCOL",
} as const;

export interface NativeConfig {
	sessionID: string;
	instanceID: string;
	endpoint: string;
	protocol: number;
}

export interface ExtensionApiLike {
	on(event: string, handler: (event: any, ctx?: any) => unknown): unknown;
	sendUserMessage(
		text: string,
		options: { deliverAs: "followUp"; expandPromptTemplates: false },
	): unknown;
}

export interface BridgeOptions extends NativeConfig {
	sendUserMessage: (text: string) => unknown;
	log?: (event: string, data?: Record<string, unknown>) => void;
}

type Outcome = "accepted" | "not_sent";

/**
 * Incremental JSONL frame reader. Bytes are buffered until a newline, so a
 * UTF-8 sequence split across chunks is decoded only after the whole line is
 * available. MAX_FRAME_BYTES includes the newline.
 */
export class FrameDecoder {
	private buffer = Buffer.alloc(0);

	push(chunk: Buffer): { lines: string[]; overflow: boolean } {
		this.buffer = this.buffer.length === 0 ? chunk : Buffer.concat([this.buffer, chunk]);
		const lines: string[] = [];
		while (true) {
			const newline = this.buffer.indexOf(0x0a);
			if (newline === -1) break;
			const frame = this.buffer.subarray(0, newline + 1);
			this.buffer = this.buffer.subarray(newline + 1);
			if (frame.length > MAX_FRAME_BYTES) return { lines, overflow: true };
			lines.push(frame.subarray(0, newline).toString("utf8"));
		}
		if (this.buffer.length >= MAX_FRAME_BYTES) return { lines, overflow: true };
		return { lines, overflow: false };
	}
}

export function acceptedIDsPath(endpoint: string, instanceID: string): string {
	return join(dirname(endpoint), `accepted-${instanceID}.ids`);
}

export function configFromEnv(
	env: Record<string, string | undefined> = process.env,
): NativeConfig | undefined {
	const sessionID = env[NATIVE_ENV.session];
	const instanceID = env[NATIVE_ENV.instance];
	const endpoint = env[NATIVE_ENV.endpoint];
	const protocol = Number(env[NATIVE_ENV.protocol] ?? String(PROTOCOL_VERSION));
	if (!sessionID || !instanceID || !endpoint || protocol !== PROTOCOL_VERSION) return undefined;
	return { sessionID, instanceID, endpoint, protocol };
}

export class DeliveryBridge {
	readonly config: NativeConfig;
	private readonly sendUserMessage: (text: string) => unknown;
	private readonly log: (event: string, data?: Record<string, unknown>) => void;
	private readonly acceptedFile: string;
	private server: net.Server | undefined;
	private connections = new Set<net.Socket>();
	private accepted = new Set<number>();
	private modalOpen = false;
	private compaction = false;
	private suppressStart = false;
	private context: any;

	constructor(options: BridgeOptions) {
		this.config = {
			sessionID: options.sessionID,
			instanceID: options.instanceID,
			endpoint: options.endpoint,
			protocol: options.protocol,
		};
		this.sendUserMessage = options.sendUserMessage;
		this.log = options.log ?? (() => {});
		this.acceptedFile = acceptedIDsPath(options.endpoint, options.instanceID);
	}

	get listening(): boolean {
		return this.server?.listening === true;
	}

	get compactionActive(): boolean {
		return this.compaction;
	}

	setModal(open: boolean): void {
		this.modalOpen = open;
	}

	setCompaction(active: boolean): void {
		this.compaction = active;
	}

	/**
	 * Bind the bridge to the Pi history reported by the supported extension
	 * context. The ROCKET_* environment alone can outlive a switch: after
	 * /reload a fresh module instance still sees the old launch env.
	 */
	bindContext(ctx: any): boolean {
		let sessionID: unknown;
		try {
			sessionID = ctx?.sessionManager?.getSessionId?.();
		} catch {
			sessionID = undefined;
		}
		if (typeof sessionID !== "string" || sessionID !== this.config.sessionID) return false;
		this.context = ctx;
		return true;
	}

	/** The history Pi would submit into must still be the bound one. */
	historyMatches(): boolean {
		if (!this.context) return false;
		try {
			return this.context.sessionManager.getSessionId() === this.config.sessionID;
		} catch {
			return false;
		}
	}

	async start(): Promise<void> {
		if (this.suppressStart || this.server?.listening) return;
		mkdirSync(dirname(this.config.endpoint), { recursive: true, mode: 0o700 });
		chmodSync(dirname(this.config.endpoint), 0o700);
		try {
			unlinkSync(this.config.endpoint);
		} catch {
			// A stale socket from a killed launch is expected.
		}
		this.loadAccepted();
		await new Promise<void>((resolve, reject) => {
			const server = net.createServer((socket) => this.handleConnection(socket));
			const onError = (error: Error) => {
				server.close();
				reject(error);
			};
			server.once("error", onError);
			server.listen(this.config.endpoint, () => {
				server.removeListener("error", onError);
				server.on("error", (error) => this.log("server_error", { error: String(error) }));
				try {
					chmodSync(this.config.endpoint, 0o600);
				} catch (error) {
					this.log("chmod_error", { error: String(error) });
				}
				this.server = server;
				resolve();
			});
		});
	}

	/** Start from an extension event without an unhandled rejection. */
	startSafely(): void {
		void this.start().catch((error) => this.log("start_error", { error: String(error) }));
	}

	stop(reason = ""): void {
		// A reload keeps this live launch and its dedupe state. Any other
		// lifecycle end (quit/new/resume/fork/switch) must never reopen the
		// same identity against a changed context.
		if (reason !== "reload") this.suppressStart = true;
		const server = this.server;
		this.server = undefined;
		if (server) server.close();
		for (const socket of this.connections) socket.destroy();
		this.connections.clear();
		if (reason !== "reload") {
			// On reload a new extension instance may already have bound the
			// same path; leave the stale file to be replaced by its start().
			try {
				unlinkSync(this.config.endpoint);
			} catch {
				// Already gone.
			}
			try {
				unlinkSync(this.acceptedFile);
			} catch {
				// No accepted IDs yet.
			}
		}
	}

	private loadAccepted(): void {
		let data: string;
		try {
			data = readFileSync(this.acceptedFile, "utf8");
		} catch {
			return;
		}
		for (const line of data.split("\n")) {
			const id = Number(line);
			if (Number.isSafeInteger(id) && id > 0) this.accepted.add(id);
		}
	}

	private persistAccepted(messageID: number): void {
		try {
			appendFileSync(this.acceptedFile, `${messageID}\n`, { mode: 0o600 });
			chmodSync(this.acceptedFile, 0o600);
		} catch (error) {
			// The API call already happened; losing the dedupe record must not
			// turn a possible submission into a false NotSent.
			this.log("persist_error", { message_id: messageID, error: String(error) });
		}
	}

	private handleConnection(socket: net.Socket): void {
		this.connections.add(socket);
		const decoder = new FrameDecoder();
		const close = () => this.connections.delete(socket);
		socket.on("close", close);
		socket.on("error", close);
		socket.on("data", (chunk: Buffer) => {
			const { lines, overflow } = decoder.push(chunk);
			if (overflow) {
				this.log("frame_overflow");
				socket.end(`${this.responseLine(undefined, "not_sent")}\n`);
				return;
			}
			for (const line of lines) socket.write(`${this.handleLine(line)}\n`);
			if (lines.length > 0) socket.end();
		});
	}

	private handleLine(line: string): string {
		let request: any;
		try {
			request = JSON.parse(line);
		} catch {
			return this.responseLine(undefined, "not_sent");
		}
		if (!this.validRequest(request)) return this.responseLine(request, "not_sent");
		// The request must address the history Pi is in right now, not just
		// the one named by the launch environment.
		if (!this.historyMatches()) return this.responseLine(request, "not_sent");
		if (this.accepted.has(request.message_id)) return this.responseLine(request, "accepted");
		// Only known states refuse before the API. isIdle alone cannot
		// distinguish ordinary busy from compaction, so it is not a guard.
		if (this.modalOpen || this.compaction) return this.responseLine(request, "not_sent");

		try {
			this.sendUserMessage(request.text);
		} catch (error) {
			// The call may already have reached the agent. Never report
			// not_sent after a possible submission.
			this.log("send_error", { message_id: request.message_id, error: String(error) });
		}
		this.accepted.add(request.message_id);
		this.persistAccepted(request.message_id);
		return this.responseLine(request, "accepted");
	}

	private validRequest(request: any): boolean {
		return (
			typeof request === "object" &&
			request !== null &&
			request.v === PROTOCOL_VERSION &&
			request.session_id === this.config.sessionID &&
			request.instance_id === this.config.instanceID &&
			Number.isSafeInteger(request.message_id) &&
			request.message_id > 0 &&
			typeof request.text === "string"
		);
	}

	private responseLine(request: any, outcome: Outcome): string {
		const sessionID = typeof request?.session_id === "string" ? request.session_id : this.config.sessionID;
		const instanceID = typeof request?.instance_id === "string" ? request.instance_id : this.config.instanceID;
		const messageID = Number.isSafeInteger(request?.message_id) ? request.message_id : 0;
		return JSON.stringify({
			v: PROTOCOL_VERSION,
			session_id: sessionID,
			instance_id: instanceID,
			message_id: messageID,
			outcome,
		});
	}
}

export function createDeliveryExtension(
	api: ExtensionApiLike,
	env: Record<string, string | undefined> = process.env,
): DeliveryBridge | undefined {
	const config = configFromEnv(env);
	if (!config) return undefined;
	const bridge = new DeliveryBridge({
		...config,
		sendUserMessage: (text) =>
			api.sendUserMessage(text, { deliverAs: "followUp", expandPromptTemplates: false }),
	});
	api.on("session_start", (_event: any, ctx: any) => {
		if (!bridge.bindContext(ctx)) {
			// A reloaded module with a stale env must never open the old
			// target inside a different history. Neutralize the old path.
			bridge.stop("foreign-history");
			return;
		}
		bridge.startSafely();
	});
	api.on("session_shutdown", (event: any) => {
		bridge.stop(String(event?.reason ?? ""));
	});
	api.on("session_before_switch", () => bridge.stop("switch"));
	api.on("ui_prompt_start", () => bridge.setModal(true));
	api.on("ui_prompt_end", () => bridge.setModal(false));
	api.on("session_before_compact", () => bridge.setCompaction(true));
	api.on("session_compact", () => bridge.setCompaction(false));
	api.on("session_compact_failed", () => bridge.setCompaction(false));
	return bridge;
}

export default function deliveryExtension(api: ExtensionAPI): void {
	createDeliveryExtension(api as unknown as ExtensionApiLike);
}
