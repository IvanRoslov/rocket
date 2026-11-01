import { appendFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// Research-only fixture. Simulates an unrelated, earlier-loaded extension whose
// ui_prompt_start/end handler is asynchronous. It never changes UI state.
const logPath = process.env.PI_TIMING_SLOW_LOG!;
mkdirSync(dirname(logPath), { recursive: true });
const log = (event: string, data: Record<string, unknown> = {}) => {
	appendFileSync(logPath, `${JSON.stringify({ event, time: Date.now(), ...data })}\n`);
};

const delayMs = Number(process.env.PI_TIMING_SLOW_MS ?? "1200");
const sleep = () => new Promise((resolve) => setTimeout(resolve, delayMs));

export default function (api: ExtensionAPI) {
	api.on("ui_prompt_start", async (event) => {
		log("slow_start_begin", { kind: event.kind });
		await sleep();
		log("slow_start_end", { kind: event.kind });
	});
	api.on("ui_prompt_end", async (event) => {
		log("slow_end_begin", { kind: event.kind });
		await sleep();
		log("slow_end_end", { kind: event.kind });
	});
}
