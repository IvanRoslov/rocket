import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// Rocket's native delivery extension. The production bridge is implemented in
// task 2B; this module is embedded into the Rocket binary and loaded with an
// explicit --extension flag only for supported Pi 1.4+ launches.
export default function deliveryExtension(_api: ExtensionAPI): void {}
