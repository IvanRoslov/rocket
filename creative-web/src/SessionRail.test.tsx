import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
