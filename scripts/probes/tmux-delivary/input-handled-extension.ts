import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// Research-only fixture for the Q5 correlation evidence. It never touches the
// editor or the message text. The FIRST extension-sourced input identical to
// PI_INPUT_HANDLED_TEXT is answered with the supported public result
// {action:"handled"} (Pi returns before appending a user record); the SECOND
// identical input continues normally and is recorded. Both observations are
// logged for the scenario to compare against the transcript.

const logPath = process.env.PI_INPUT_HANDLED_LOG!;
const target = process.env.PI_INPUT_HANDLED_TEXT ?? "";
const firstMode = process.env.PI_INPUT_HANDLED_FIRST === "1";
mkdirSync(dirname(logPath), { recursive: true });
const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

export default function inputHandledExtension(api: ExtensionAPI) {
	let handled = 0;
	api.on("input", (event) => {
		log("input", {
			text: event.text,
			source: event.source,
			streamingBehavior: event.streamingBehavior ?? null,
			identical: event.text === target,
		});
		if (event.source !== "extension") return { action: "continue" };
		if (firstMode) {
			// Queue E2E mode: the first extension-sourced input is handled,
			// every later one continues.
			if (handled === 0) {
				handled += 1;
				log("handled", { text: event.text });
				return { action: "handled" };
			}
			log("continued", { text: event.text });
			return { action: "continue" };
		}
		if (event.text !== target) {
			log("continued", { text: event.text });
			return { action: "continue" };
		}
		if (handled === 0) {
			handled += 1;
			log("handled", { text: event.text });
			return { action: "handled" };
		}
		log("continued", { text: event.text });
		return { action: "continue" };
	});
}
