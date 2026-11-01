import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import net from "node:net";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

import {
	DeliveryBridge,
	FrameDecoder,
	MAX_FRAME_BYTES,
	acceptedIDsPath,
	createDeliveryExtension,
	retiredLaunchPath,
} from "./delivery-extension.ts";

class FakeApi {
	handlers = new Map<string, ((event: any, ctx?: any) => unknown)[]>();
	calls: { text: string; options: any }[] = [];
	throwOnSend = false;
	sessionID = "sess-1";
	historyThrows = false;
	ctxOverride: any;
	onSend?: (text: string) => void;

	readonly ctx = {
		isIdle: () => true,
		sessionManager: {
			getSessionId: (): string => {
				if (this.historyThrows) throw new Error("history unavailable");
				return this.sessionID;
			},
		},
	};

	on(event: string, handler: (event: any, ctx?: any) => unknown): () => void {
		const list = this.handlers.get(event) ?? [];
		list.push(handler);
		this.handlers.set(event, list);
		return () => {};
	}

	sendUserMessage(text: string, options: any): void {
		this.onSend?.(text);
		this.calls.push({ text, options });
		if (this.throwOnSend) throw new Error("send failed after submission");
	}

	emit(event: string, payload: Record<string, unknown> = {}): void {
		for (const handler of this.handlers.get(event) ?? []) {
			handler(payload, this.ctxOverride ?? this.ctx);
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
	setNativeEnv(endpoint, sessionID, instanceID);
	api.sessionID = sessionID;
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge, "delivery extension must start from the native env");
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "startup" });
	await waitForListening(bridge);
	return { bridge, api, endpoint, sessionID, instanceID };
}

function setNativeEnv(endpoint: string, sessionID = "sess-1", instanceID = "instance-1"): void {
	process.env.ROCKET_PI_NATIVE_SESSION_ID = sessionID;
	process.env.ROCKET_PI_NATIVE_INSTANCE_ID = instanceID;
	process.env.ROCKET_PI_NATIVE_ENDPOINT = endpoint;
	process.env.ROCKET_PI_NATIVE_PROTOCOL = "1";
}

async function waitForListening(bridge: { listening: boolean }): Promise<void> {
	for (let i = 0; i < 100 && !bridge.listening; i++) {
		await new Promise((resolve) => setTimeout(resolve, 5));
	}
	assert.equal(bridge.listening, true, "a bound session_start must open the endpoint");
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

function nativeFrame(sessionID: string, instanceID: string, messageID: number, text: string): Buffer {
	return frameFor({ v: 1, session_id: sessionID, instance_id: instanceID, message_id: messageID, text });
}

function sendTrailingFrame(endpoint: string, first: Buffer, second: Buffer): Promise<string> {
	return new Promise((resolve) => {
		const socket = net.createConnection(endpoint);
		let data = "";
		socket.on("connect", () => socket.write(first));
		socket.once("data", () => {
			// The response for the first frame arrived; try one more frame on
			// the same connection after the server ended its side.
			try {
				socket.write(second);
			} catch {
				// A fully closed connection is also acceptable.
			}
		});
		socket.on("data", (chunk) => {
			data += chunk.toString("utf8");
		});
		socket.on("error", () => {});
		setTimeout(() => {
			socket.destroy();
			resolve(data);
		}, 100);
	});
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
	assert.ok(existsSync(ids), "a shutdown must not destroy the launch dedupe state");
	assert.ok(existsSync(retiredLaunchPath(endpoint, instanceID)), "a non-reload shutdown retires the launch");

	const third = await makeBridge(t, new FakeApi());
	third.api.emit("session_before_switch");
	third.api.emit("session_shutdown", { reason: "new" });
	third.api.emit("session_start", { reason: "new" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(third.bridge.listening, false, "a committed switch must not reopen the old identity");
});

test("refusesStartInAForeignHistory", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "history-b";
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "a foreign history must not open the endpoint");
	assert.equal(existsSync(endpoint), false, "a foreign history must not leave a reachable endpoint");
	await assert.rejects(connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 71, text: "чужой контекст",
	}));
	assert.equal(api.calls.length, 0);
});

test("refusesStartWithoutHistoryContext", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint);
	api.ctxOverride = { isIdle: () => true };
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "startup" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "without a supported history getter the bridge fails closed");
	assert.equal(api.calls.length, 0);
});

test("reloadAfterSwitchNeverRoutesIntoNewHistory", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	assert.equal((await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 81, text: "история A",
	})).outcome, "accepted");
	api.emit("session_before_switch", { reason: "new" });
	assert.equal(api.calls.length, 1);

	// Pi reloads the extension module in history B while the old ROCKET env
	// still names history A. The new instance must not open A's endpoint.
	const foreignApi = new FakeApi();
	foreignApi.sessionID = "history-b";
	const foreign = createDeliveryExtension(foreignApi);
	assert.ok(foreign);
	t.after(() => foreign.stop("quit"));
	foreignApi.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(foreign.listening, false);
	await assert.rejects(connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 82, text: "история B",
	}));
	assert.equal(foreignApi.calls.length, 0, "the old target must make zero API calls after the switch");
});

test("submissionRequiresBoundHistory", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	api.sessionID = "history-b";
	const response = await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 91, text: "уже не та история",
	});
	assert.equal(response.outcome, "not_sent");
	assert.equal(api.calls.length, 0);
});

test("historyGetterFailureFailsClosed", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	api.historyThrows = true;
	const response = await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 92, text: "нет истории",
	});
	assert.equal(response.outcome, "not_sent");
	assert.equal(api.calls.length, 0);
});

test("reservesAcceptedIDBeforeAPI", async (t) => {
	const api = new FakeApi();
	const { endpoint, instanceID } = await makeBridge(t, api);
	const ids = acceptedIDsPath(endpoint, instanceID);
	let observedDuringSend = "";
	api.onSend = () => {
		observedDuringSend = readFileSync(ids, "utf8");
	};
	const response = await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 101, text: "резерв",
	});
	assert.equal(response.outcome, "accepted");
	assert.match(observedDuringSend, /^101$/m, "the accepted ID must be durable before the API call");
});

test("unreadableAcceptedStateFailsClosed", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "sess-1";
	mkdirSync(acceptedIDsPath(endpoint, "instance-1"), { recursive: true, mode: 0o700 });
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "an unreadable accepted set must not become empty");
	await assert.rejects(connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 102, text: "нет состояния",
	}));
	assert.equal(api.calls.length, 0);
});

test("corruptAcceptedStateFailsClosed", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "sess-1";
	writeFileSync(acceptedIDsPath(endpoint, "instance-1"), "not-a-message-id\n", { mode: 0o600 });
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "corrupt accepted state must not become empty");
	assert.equal(api.calls.length, 0);
});

test("persistenceFailureReturnsNotSentWithoutAPI", async (t) => {
	if (process.getuid?.() === 0) {
		t.skip("root bypasses file permission bits");
		return;
	}
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "sess-1";
	const ids = acceptedIDsPath(endpoint, "instance-1");
	writeFileSync(ids, "5\n", { mode: 0o600 });
	chmodSync(ids, 0o400);
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await waitForListening(bridge);
	const response = await connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 103, text: "не записан",
	});
	assert.equal(response.outcome, "not_sent", "no durable reservation means no submission");
	assert.equal(api.calls.length, 0);
});

test("oneExchangePerConnection", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	const first = nativeFrame(sessionID, instanceID, 201, "первый");
	const second = nativeFrame(sessionID, instanceID, 202, "второй");
	const response = await connectRequest(endpoint, null, [Buffer.concat([first, second])]);
	assert.equal(response.message_id, 201);
	assert.equal(api.calls.length, 1, "only the first frame of a chunk may reach the API");
	assert.equal(api.calls[0].text, "первый");
});

test("trailingChunkCannotSubmit", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	const first = nativeFrame(sessionID, instanceID, 211, "первый");
	const second = nativeFrame(sessionID, instanceID, 212, "второй");
	const received = await sendTrailingFrame(endpoint, first, second);
	assert.match(received, /"message_id":211/);
	assert.equal(api.calls.length, 1, "a later chunk after the response must not reach the API");
});

test("socketPermissionFailureFailsClosed", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	const bridge = new DeliveryBridge({
		sessionID: "sess-1",
		instanceID: "instance-1",
		endpoint,
		protocol: 1,
		sendUserMessage: (text) => api.sendUserMessage(text, { deliverAs: "followUp", expandPromptTemplates: false }),
		chmod: (path, mode) => {
			if (path === endpoint) throw new Error("chmod denied");
			chmodSync(path, mode);
		},
	});
	t.after(() => bridge.stop("quit"));
	await assert.rejects(bridge.start(), /chmod denied/);
	assert.equal(bridge.listening, false, "an endpoint without the 0600 contract must not accept");
	assert.equal(existsSync(endpoint), false, "the refused endpoint must be unlinked");
	assert.equal(api.calls.length, 0);
});

test("retiredLaunchDoesNotReopenAfterSwitchAndReturn", async (t) => {
	const api = new FakeApi();
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);
	assert.equal((await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 301, text: "до переключения",
	})).outcome, "accepted");
	assert.equal(api.calls.length, 1);

	// The switch commits: the old runtime shuts down and another history starts.
	api.emit("session_before_switch", { reason: "new" });
	api.emit("session_shutdown", { reason: "new" });
	assert.ok(
		existsSync(acceptedIDsPath(endpoint, instanceID)),
		"switch must not destroy the launch dedupe state",
	);

	const foreignApi = new FakeApi();
	foreignApi.sessionID = "history-b";
	const foreign = createDeliveryExtension(foreignApi);
	assert.ok(foreign);
	t.after(() => foreign.stop("quit"));
	foreignApi.emit("session_start", { reason: "new" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(foreign.listening, false);
	assert.equal(foreignApi.calls.length, 0);

	// Returning to the same history must not resurrect the retired launch.
	const backApi = new FakeApi();
	backApi.sessionID = sessionID;
	const back = createDeliveryExtension(backApi);
	assert.ok(back);
	t.after(() => back.stop("quit"));
	backApi.emit("session_start", { reason: "resume" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(back.listening, false, "a retired launch must not reopen");
	await assert.rejects(connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 302, text: "после возврата",
	}));
	assert.equal(backApi.calls.length, 0);
});

test("truncatedAcceptedTailFailsClosed", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "sess-1";
	// A crash mid-append leaves a partial record; it must never fold into ID 4.
	writeFileSync(acceptedIDsPath(endpoint, "instance-1"), "4", { mode: 0o600 });
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "an unterminated tail must not load as an ID");
	assert.equal(api.calls.length, 0);
});

test("nonCanonicalAcceptedRecordsFailClosed", async (t) => {
	for (const record of ["0x10\n", "007\n", "+4\n", "4.0\n", " 4\n"]) {
		const api = new FakeApi();
		const endpoint = endpointFor();
		setNativeEnv(endpoint, "sess-1", "instance-1");
		api.sessionID = "sess-1";
		writeFileSync(acceptedIDsPath(endpoint, "instance-1"), record, { mode: 0o600 });
		const bridge = createDeliveryExtension(api);
		assert.ok(bridge);
		t.after(() => bridge.stop("quit"));
		api.emit("session_start", { reason: "reload" });
		await new Promise((resolve) => setTimeout(resolve, 20));
		assert.equal(bridge.listening, false, `record ${JSON.stringify(record)} must fail closed`);
		assert.equal(api.calls.length, 0);
	}
});

test("endpointDirectoryBindFailureIsContained", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	mkdirSync(endpoint, { recursive: true, mode: 0o700 });
	setNativeEnv(endpoint);
	api.sessionID = "sess-1";
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	assert.doesNotThrow(() => api.emit("session_start"));
	await new Promise((resolve) => setTimeout(resolve, 30));
	assert.equal(bridge.listening, false, "a bind failure must not accept");
	assert.equal(api.calls.length, 0);
});

test("danglingAcceptedSymlinkFailsClosed", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint, "sess-1", "instance-1");
	api.sessionID = "sess-1";
	symlinkSync(join(shortDir(), "missing", "state"), acceptedIDsPath(endpoint, "instance-1"));
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start", { reason: "reload" });
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false, "an unreadable accepted path must not become empty");
	assert.equal(api.calls.length, 0);
});

test("deferredHandlerErrorAdmitsNoRecord", async (t) => {
	// Q5 fixture: the supported Pi API accepts sendUserMessage and returns
	// void. A later handler error can drop the turn, so an accepted call may
	// leave no user record; a later call with the same text can leave the
	// only one. The bridge cannot report NotSent after either call.
	const api = new FakeApi();
	const journal: string[] = [];
	let deferNext = false;
	const recordSend = (text: string, options: any): void => {
		api.calls.push({ text, options });
		if (deferNext) {
			deferNext = false;
			setTimeout(() => {
				// Deferred handler error: the turn is dropped, no record.
			}, 0);
			return;
		}
		journal.push(text);
	};
	api.sendUserMessage = recordSend;
	const { endpoint, sessionID, instanceID } = await makeBridge(t, api);

	deferNext = true;
	const first = await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 401, text: "одинаковый",
	});
	const second = await connectRequest(endpoint, {
		v: 1, session_id: sessionID, instance_id: instanceID, message_id: 402, text: "одинаковый",
	});
	await new Promise((resolve) => setTimeout(resolve, 10));

	assert.equal(first.outcome, "accepted", "a possible submission is never reported as not_sent");
	assert.equal(second.outcome, "accepted");
	assert.equal(api.calls.length, 2);
	assert.deepEqual(journal, ["одинаковый"], "only the second call left a record");
});

test("sessionStartOpensPrivateEndpoint", async (t) => {
	const api = new FakeApi();
	const endpoint = endpointFor();
	setNativeEnv(endpoint);
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	api.emit("session_start");
	await waitForListening(bridge);
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
	setNativeEnv(endpoint);
	const bridge = createDeliveryExtension(api);
	assert.ok(bridge);
	t.after(() => bridge.stop("quit"));
	assert.doesNotThrow(() => api.emit("session_start"));
	await new Promise((resolve) => setTimeout(resolve, 20));
	assert.equal(bridge.listening, false);
	await assert.rejects(connectRequest(endpoint, {
		v: 1, session_id: "sess-1", instance_id: "instance-1", message_id: 62, text: "старт не удался",
	}));
	assert.equal(api.calls.length, 0);
});

test("frameDecoderKeepsANewlineInsideTheByteLimit", () => {
	const decoder = new FrameDecoder();
	const body = Buffer.alloc(MAX_FRAME_BYTES - 1, 0x61);
	const result = decoder.push(Buffer.concat([body, Buffer.from("\n")]));
	assert.deepEqual(result, { lines: [body.toString("utf8")], overflow: false });
	const tooLarge = new FrameDecoder().push(Buffer.alloc(MAX_FRAME_BYTES, 0x61));
	assert.deepEqual(tooLarge, { lines: [], overflow: true });
});
