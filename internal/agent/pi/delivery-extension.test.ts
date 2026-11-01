import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import net from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

import {
	FrameDecoder,
	MAX_FRAME_BYTES,
	acceptedIDsPath,
	createDeliveryExtension,
} from "./delivery-extension.ts";

class FakeApi {
	handlers = new Map<string, ((event: any, ctx?: any) => unknown)[]>();
	calls: { text: string; options: any }[] = [];
	throwOnSend = false;

	on(event: string, handler: (event: any, ctx?: any) => unknown): () => void {
		const list = this.handlers.get(event) ?? [];
		list.push(handler);
		this.handlers.set(event, list);
		return () => {};
	}

	sendUserMessage(text: string, options: any): void {
		this.calls.push({ text, options });
		if (this.throwOnSend) throw new Error("send failed after submission");
	}

	emit(event: string, payload: Record<string, unknown> = {}): void {
		for (const handler of this.handlers.get(event) ?? []) {
			handler(payload, { isIdle: () => true });
		}
	}
}

let socketCounter = 0;
const cleanups: string[] = [];

function shortDir(): string {
	const dir = mkdtempSync(join("/tmp", "rpi-"));
	cleanups.push(dir);
	return dir;
}

test.after(() => {
	for (const dir of cleanups.splice(0)) rmSync(dir, { recursive: true, force: true });
});

function endpointFor(): string {
	const dir = shortDir();
	mkdirSync(dir, { recursive: true, mode: 0o700 });
	return join(dir, `s${socketCounter++}`);
}

async function makeBridge(
	t: test.TestContext,
	api: FakeApi,
	overrides: Partial<{ sessionID: string; instanceID: string; endpoint: string }> = {},
) {
	const endpoint = overrides.endpoint ?? endpointFor();
	const sessionID = overrides.sessionID ?? "sess-1";
	const instanceID = overrides.instanceID ?? "instance-1";
	process.env.ROCKET_PI_NATIVE_SESSION_ID = sessionID;
	process.env.ROCKET_PI_NATIVE_INSTANCE_ID = instanceID;
	process.env.ROCKET_PI_NATIVE_ENDPOINT = endpoint;
	process.env.ROCKET_PI_NATIVE_PROTOCOL = "1";
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge, "delivery extension must start from the native env");
	t.after(() => bridge.stop("quit"));
	await bridge.start();
	return { bridge, api, endpoint, sessionID, instanceID };
}

function connectRequest(endpoint: string, body: unknown, chunks?: Buffer[]): Promise<any> {
	return new Promise((resolve, reject) => {
		const socket = net.createConnection(endpoint);
		let data = "";
		socket.on("connect", () => {
			const frame = Buffer.from(`${JSON.stringify(body)}\n`);
			if (!chunks) {
				socket.write(frame);
				return;
			}
			let offset = 0;
			const next = () => {
				if (offset >= chunks.length) return;
				socket.write(chunks[offset++]);
				setTimeout(next, 5);
			};
			next();
		});
		socket.on("data", (chunk) => {
			data += chunk.toString("utf8");
		});
		socket.on("end", () => {
			try {
				resolve(JSON.parse(data.trim()));
			} catch (error) {
				reject(error);
			}
		});
		socket.on("error", reject);
	});
}

function frameFor(body: unknown): Buffer {
	return Buffer.from(`${JSON.stringify(body)}\n`);
}

test("rejectsWrongSessionOrInstance", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID } = await makeBridge(t, api);
	for (const body of [
		{ v: 1, session_id: "other", instance_id: "instance-1", message_id: 1, text: "привет" },
		{ v: 1, session_id: sessionID, instance_id: "other", message_id: 2, text: "привет" },
		{ v: 2, session_id: sessionID, instance_id: "instance-1", message_id: 3, text: "привет" },
		{ v: 1, session_id: sessionID, instance_id: "instance-1", message_id: 0, text: "привет" },
	]) {
		const response = await connectRequest(endpoint, body);
		assert.equal(response.outcome, "not_sent");
	}
	assert.equal(api.calls.length, 0);
});

test("boundsFramesByBytes", async (t) => {
	const api = new FakeApi();
	const { endpoint } = await makeBridge(t, api);
	const oversized = Buffer.concat([Buffer.alloc(MAX_FRAME_BYTES, 0x61), Buffer.from("\n")]);
	const response = await connectRequest(endpoint, null, [oversized]);
	assert.equal(response.outcome, "not_sent");
	assert.equal(api.calls.length, 0);
});

test("decodesSplitUTF8", async (t) => {
	const api = new FakeApi();
	const { endpoint } = await makeBridge(t, api);
	const body = { v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 7, text: "Привет, мир" };
	const frame = frameFor(body);
	const splitAt = frame.indexOf(Buffer.from("Привет")) + 3; // inside a UTF-8 sequence
	const response = await connectRequest(endpoint, body, [frame.subarray(0, splitAt), frame.subarray(splitAt)]);
	assert.equal(response.outcome, "accepted");
	assert.equal(api.calls.length, 1);
	assert.equal(api.calls[0].text, "Привет, мир");
	assert.deepEqual(api.calls[0].options, { deliverAs: "followUp", expandPromptTemplates: false });
});

test("deduplicatesMessageID", async (t) => {
	const api = new FakeApi();
	const { endpoint } = await makeBridge(t, api);
	const body = { v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 11, text: "один" };
	assert.equal((await connectRequest(endpoint, body)).outcome, "accepted");
	assert.equal((await connectRequest(endpoint, body)).outcome, "accepted");
	assert.equal(api.calls.length, 1);
});

test("keepsDistinctEqualTexts", async (t) => {
	const api = new FakeApi();
	const { endpoint } = await makeBridge(t, api);
	for (const messageID of [21, 22]) {
		const response = await connectRequest(endpoint, {
			v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: messageID, text: "равный",
		});
		assert.equal(response.outcome, "accepted");
	}
	assert.equal(api.calls.length, 2);
});

test("knownPromptAndCompactionRefuseBeforeAPI", async (t) => {
	const api = new FakeApi();
	const { bridge, endpoint } = await makeBridge(t, api);
	const body = (id: number) => ({ v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: id, text: "текст" });

	api.emit("ui_prompt_start", { kind: "select" });
	assert.equal((await connectRequest(endpoint, body(31))).outcome, "not_sent");
	assert.equal(api.calls.length, 0);
	api.emit("ui_prompt_end", { kind: "select" });

	api.emit("session_before_compact", { reason: "manual" });
	assert.equal((await connectRequest(endpoint, body(32))).outcome, "not_sent");
	assert.equal(api.calls.length, 0);
	api.emit("session_compact", { reason: "manual" });

	assert.equal((await connectRequest(endpoint, body(33))).outcome, "accepted");
	assert.equal(api.calls.length, 1);
	assert.equal(bridge.compactionActive, false);
});

test("possibleAPICallNeverReturnsNotSent", async (t) => {
	const api = new FakeApi();
	api.throwOnSend = true;
	const { endpoint } = await makeBridge(t, api);
	const response = await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 41, text: "ошибка",
	});
	assert.equal(response.outcome, "accepted");
	assert.equal(api.calls.length, 1);
});

test("shutdownReloadAndSwitchCloseOldEndpoint", async (t) => {
	const api = new FakeApi();
	const { bridge, endpoint, instanceID } = await makeBridge(t, api);
	assert.ok(existsSync(endpoint));
	assert.equal((await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 51, text: "до reload",
	})).outcome, "accepted");
	const ids = acceptedIDsPath(endpoint, instanceID);
	assert.ok(existsSync(ids));

	api.emit("session_shutdown", { reason: "reload" });
	assert.equal(bridge.listening, false, "reload must close the old server");
	assert.ok(existsSync(ids), "reload must keep accepted IDs for the same live launch");

	const reloaded = new FakeApi();
	const second = await makeBridge(t, reloaded, { endpoint, instanceID });
	assert.equal((await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 51, text: "до reload",
	})).outcome, "accepted");
	assert.equal(reloaded.calls.length, 0, "dedupe must survive module reload");
	second.bridge.stop("quit");
	assert.equal(existsSync(ids), false);

	const third = await makeBridge(t, new FakeApi());
	third.api.emit("session_before_switch");
	third.api.emit("session_start");
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(third.bridge.listening, false, "switch must not reopen the old identity");
});

test("sessionStartOpensPrivateEndpoint", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	process.env.ROCKET_PI_NATIVE_SESSION_ID = "sess-1";
	process.env.ROCKET_PI_NATIVE_INSTANCE_ID = "instance-1";
	process.env.ROCKET_PI_NATIVE_ENDPOINT = endpoint;
	process.env.ROCKET_PI_NATIVE_PROTOCOL = "1";
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start");
	for (let i = 0; i < 100 && !bridge.listening; i++) {
		await new Promise((resolve) => setTimeout(resolve, 5));
	}
	assert.equal(bridge.listening, true);
	assert.equal(statSync(dirname(endpoint)).mode & 0o777, 0o700);
	assert.equal(statSync(endpoint).mode & 0o777, 0o600);
	const response = await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 61, text: "готово",
	});
	assert.equal(response.outcome, "accepted");
	assert.equal(api.calls.length, 1);
});

test("startFailureIsContained", async (t) => {
	const api = new FakeApi();
	const parentFile = join(shortDir(), "file");
	writeFileSync(parentFile, "not a directory");
	const endpoint = join(parentFile, "s");
	process.env.ROCKET_PI_NATIVE_SESSION_ID = "sess-1";
	process.env.ROCKET_PI_NATIVE_INSTANCE_ID = "instance-1";
	process.env.ROCKET_PI_NATIVE_ENDPOINT = endpoint;
	process.env.ROCKET_PI_NATIVE_PROTOCOL = "1";
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	assert.doesNotThrow(() => api.emit("session_start"));
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false);
});

test("frameDecoderKeepsANewlineInsideTheByteLimit", () => {
	const decoder = new FrameDecoder();
	const body = Buffer.alloc(MAX_FRAME_BYTES - 1, 0x61);
	const result = decoder.push(Buffer.concat([body, Buffer.from("\n")]));
	assert.deepEqual(result, { lines: [body.toString("utf8")], overflow: false });
	const tooLarge = new FrameDecoder().push(Buffer.alloc(MAX_FRAME_BYTES, 0x61));
	assert.deepEqual(tooLarge, { lines: [], overflow: true });
});
