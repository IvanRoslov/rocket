import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import type { SessionState } from "../../web/src/lib/types";
import { CreativeSessionRail } from "./SessionRail";

const server = setupServer(
  http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [] })),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function worker(state: SessionState) {
  return { id: `worker-${state}`, tmux_name: `worker-${state}`, kind: "worker",
    agent: "codex", state, activity: "idle" };
}
const orchestrator = { id: "creative-7", tmux_name: "creative-7", kind: "orchestrator",
  agent: "codex", state: "running", activity: "ready" };

function renderRail() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <CreativeSessionRail runID={7} orchestrator="creative-7" />
    </QueryClientProvider>,
  );
  return { ...view, client };
}

it("omits killed/done workers with stale activity while retaining live and recoverable workers", async () => {
  server.use(http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([
    orchestrator, worker("killed"), worker("done"), worker("running"), worker("spawning"), worker("errored"),
  ])));
  renderRail();
  await screen.findByText("worker-running");
  expect(screen.queryByText("worker-killed")).not.toBeInTheDocument();
  expect(screen.queryByText("worker-done")).not.toBeInTheDocument();
  for (const id of ["worker-running", "worker-spawning", "worker-errored"]) {
    expect(screen.getByText(id)).toBeVisible();
    const card = screen.getByText(id).closest<HTMLElement>(".session-rail__worker")!;
    expect(within(card).getByRole("link", { name: /term/ })).toHaveAttribute("href", `/term/${id}`);
    expect(within(card).getByRole("link", { name: /chat/ })).toHaveAttribute("href", `/chat/${id}`);
  }
  expect(screen.queryByText("No workers yet.")).not.toBeInTheDocument();
});

it.each([{ label: "no workers", workers: [] }, { label: "only closed workers", workers: [worker("killed"), worker("done")] }])("shows the empty worker panel for $label", async ({ workers }) => {
  server.use(http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([orchestrator, ...workers])));
  const { container } = renderRail();
  expect(await screen.findByText("No workers yet.")).toBeVisible();
  expect(container.querySelectorAll(".session-rail__worker")).toHaveLength(0);
  expect(screen.getByText("creative-7")).toBeVisible();
});

it.each(["killed", "done"] as const)("removes a worker after a refreshed %s state while retaining its full cached record", async (state) => {
  let sessions = [orchestrator, worker("running")];
  server.use(http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json(sessions)));
  const { client } = renderRail();
  await screen.findByText("worker-running");
  sessions = [orchestrator, { ...worker("running"), state }];
  await act(async () => {
    await client.refetchQueries({ queryKey: ["creative", "sessions", 7] });
  });
  await waitFor(() => expect(screen.queryByText("worker-running")).not.toBeInTheDocument());
  expect(screen.getByText("No workers yet.")).toBeVisible();
  expect(client.getQueryData(["creative", "sessions", 7])).toEqual(sessions);
});

it("keeps the offline orchestrator and restore action when all workers are closed", async () => {
  let restored = false;
  server.use(
    http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([
      { ...orchestrator, state: restored ? "running" : "killed" }, worker("killed"),
    ])),
    http.post("/v1/sessions/creative-7/restore", () => {
      restored = true;
      return HttpResponse.json({ ...orchestrator });
    }),
  );
  renderRail();
  const restore = await screen.findByRole("button", { name: "Restore creative orchestrator" });
  expect(screen.getByText("creative-7")).toBeVisible();
  expect(screen.getByText("Offline")).toBeVisible();
  await userEvent.click(restore);
  await screen.findByText("Connected");
  await waitFor(() => expect(screen.queryByRole("button", { name: "Restore creative orchestrator" })).not.toBeInTheDocument());
  expect(screen.getByText("No workers yet.")).toBeVisible();
});


const registry = [
  { name: "director", model: "model-d", enabled: true },
  { name: "writer", model: "model-w", enabled: true },
  { name: "retired", model: "model-r", enabled: false },
  { name: "unrelated", model: "model-u", enabled: false },
];
function setupProfiles() {
  server.use(
    http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: registry })),
    http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([])),
  );
}
function renderModels(launch = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function RunRail() {
    const run = useQuery({ queryKey: ["creative", "run", 7],
      queryFn: async () => (await fetch("/v1/creative/runs/7")).json() });
    if (!run.data) return null;
    return <CreativeSessionRail runID={7} orchestrator={launch ? "" : "creative-7"}
      allowedProfiles={run.data.allowed_profiles} />;
  }
  return { ...render(<QueryClientProvider client={client}><RunRail /></QueryClientProvider>), client };
}

it("launches with selected worker profiles and keeps the launch draft on error", async () => {
  setupProfiles();
  const launches: unknown[] = [];
  let fail = true;
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json({ allowed_profiles: [] })),
    http.post("/v1/creative/runs/7/sessions", async ({ request }) => {
      launches.push(await request.json());
      return fail ? HttpResponse.json({ error: { code: "internal_error", message: "Launch failed" } }, { status: 500 })
        : HttpResponse.json({ id: "creative-7" });
    }),
  );
  renderModels(true);
  const director = await screen.findByRole("checkbox", { name: /director.*model-d/ });
  expect(director).not.toBeChecked();
  expect(screen.getByRole("checkbox", { name: /writer.*model-w/ })).not.toBeChecked();
  expect(screen.queryByRole("checkbox", { name: /retired/ })).not.toBeInTheDocument();
  await userEvent.selectOptions(screen.getByLabelText("Model profile"), "director");
  await userEvent.click(screen.getByRole("checkbox", { name: /writer.*model-w/ }));
  await userEvent.click(screen.getByRole("button", { name: "Start creative orchestrator" }));
  await screen.findByText("Launch failed");
  expect(screen.getByRole("checkbox", { name: /writer/ })).toBeChecked();
  expect(screen.getByLabelText("Model profile")).toHaveValue("director");
  fail = false;
  await userEvent.click(screen.getByRole("button", { name: "Start creative orchestrator" }));
  await waitFor(() => expect(launches).toEqual([
    { profile: "director", allowed_profiles: ["writer"] },
    { profile: "director", allowed_profiles: ["writer"] },
  ]));
});

it("edits the complete saved list, removes a disabled profile and displays the saved result", async () => {
  setupProfiles();
  let allowed = ["director", "retired"];
  const saves: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json({ allowed_profiles: allowed })),
    http.put("/v1/creative/runs/7/models", async ({ request }) => {
      const body = await request.json() as { allowed_profiles: string[] };
      saves.push(body);
      allowed = body.allowed_profiles;
      return HttpResponse.json(body);
    }),
  );
  renderModels();
  await userEvent.click(await screen.findByRole("button", { name: "Edit worker profiles" }));
  expect(screen.getByRole("checkbox", { name: /retired.*model-r.*disabled/ })).toBeChecked();
  expect(screen.queryByRole("checkbox", { name: /unrelated/ })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("checkbox", { name: /retired/ }));
  await userEvent.click(screen.getByRole("checkbox", { name: /writer/ }));
  await userEvent.click(screen.getByRole("button", { name: "Save worker profiles" }));
  await waitFor(() => expect(saves).toEqual([{ allowed_profiles: ["director", "writer"] }]));
  await waitFor(() => expect(screen.queryByRole("checkbox")).not.toBeInTheDocument());
  const summary = screen.getByRole("region", { name: "Worker profiles" });
  expect(within(summary).getByText("director")).toBeVisible();
  expect(within(summary).getByText("writer")).toBeVisible();
  expect(within(summary).queryByText("retired")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Edit worker profiles" }));
  await userEvent.click(screen.getByRole("checkbox", { name: /director/ }));
  await userEvent.click(screen.getByRole("checkbox", { name: /writer/ }));
  await userEvent.click(screen.getByRole("button", { name: "Save worker profiles" }));
  await waitFor(() => expect(saves[1]).toEqual({ allowed_profiles: [] }));
  expect(await screen.findByText("all profiles")).toBeVisible();
});

it("preserves an edit draft on save failure and refetch, and cancels back to the saved list", async () => {
  setupProfiles();
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json({ allowed_profiles: [] })),
    http.put("/v1/creative/runs/7/models", () => HttpResponse.json({
      error: { code: "human_only", message: "Only a human can edit profiles" },
    }, { status: 403 })),
  );
  const { client } = renderModels();
  expect(await screen.findByText("all profiles")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "Edit worker profiles" }));
  await userEvent.click(screen.getByRole("checkbox", { name: /writer/ }));
  await userEvent.click(screen.getByRole("button", { name: "Save worker profiles" }));
  await screen.findByText("Only a human can edit profiles");
  await act(async () => { await client.invalidateQueries({ queryKey: ["creative"] }); });
  expect(screen.getByRole("checkbox", { name: /writer/ })).toBeChecked();
  await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getByText("all profiles")).toBeVisible();
});

it("blocks editing when the profile registry failed to load", async () => {
  server.use(
    http.get("/v1/model-profiles", () => HttpResponse.json({ error: { message: "Registry unavailable" } }, { status: 500 })),
    http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([])),
    http.get("/v1/creative/runs/7", () => HttpResponse.json({ allowed_profiles: [] })),
  );
  renderModels();
  await screen.findByText("Registry unavailable");
  expect(screen.getByRole("button", { name: "Edit worker profiles" })).toBeDisabled();
});

it("disables launch controls while profiles load and while launch is pending", async () => {
  let releaseProfiles!: () => void;
  let releaseLaunch!: () => void;
  const profilesReady = new Promise<void>((resolve) => { releaseProfiles = resolve; });
  const launchReady = new Promise<void>((resolve) => { releaseLaunch = resolve; });
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json({ allowed_profiles: [] })),
    http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([])),
    http.get("/v1/model-profiles", async () => {
      await profilesReady;
      return HttpResponse.json({ profiles: registry });
    }),
    http.post("/v1/creative/runs/7/sessions", async () => {
      await launchReady;
      return HttpResponse.json({ id: "creative-7" });
    }),
  );
  renderModels(true);
  const start = await screen.findByRole("button", { name: "Start creative orchestrator" });
  expect(start).toBeDisabled();
  expect(screen.getByLabelText("Model profile")).toBeDisabled();
  await act(async () => releaseProfiles());
  const checkbox = await screen.findByRole("checkbox", { name: /writer/ });
  await userEvent.click(checkbox);
  await userEvent.click(start);
  expect(start).toBeDisabled();
  expect(checkbox).toBeDisabled();
  expect(screen.getByLabelText("Model profile")).toBeDisabled();
  await act(async () => releaseLaunch());
  await waitFor(() => expect(start).toBeEnabled());
});
