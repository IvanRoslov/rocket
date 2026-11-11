import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { App } from "./App";
import type { Run } from "./types";
const projects = [{ id: "film", name: "Film studio" }, { id: "music", name: "Music studio" }];
const work = (id: number, project_id = "film", closure_state: Run["closure_state"] = "open"): Run => ({
  id, project_id, closure_state, pipeline_id: 3, pipeline_name: "Studio process", name: "Opening", brief: "An opening", status: "completed", orchestrator_session_id: "", stages: [],
});
let works: Run[], unread: number, unreadError: boolean, reads: unknown[], listURLs: string[];
const server = setupServer(
  http.get("/v1/projects", () => HttpResponse.json(projects)),
  http.get("/v1/creative/pipelines", () => HttpResponse.json([{ id: 3, name: "Studio process", stages: [] }])),
  http.get("/v1/creative/runs", ({ request }) => { listURLs.push(request.url); return HttpResponse.json(works); }),
  http.get("/v1/creative/runs/:id", ({ params }) => HttpResponse.json(works.find(r => r.id === Number(params.id)))),
  http.get("/v1/creative/runs/:id/feed/unread", ({ params }) => unreadError
    ? HttpResponse.json({ error: { message: "Unread service unavailable" } }, { status: 503 })
    : HttpResponse.json({ run_id: Number(params.id), total: unread, run_feed: 0, stages: [{ stage_id: 11, unread }] })),
  http.post("/v1/creative/runs/:id/feed/read", async ({ request }) => { reads.push(await request.json()); unread = 0; return HttpResponse.json({}); }),
  http.get("/v1/creative/runs/:id/feed", () => HttpResponse.json([{ id: 41, run_id: 7, stage_id: 11, item_id: 0, kind: "message", author: "orchestrator", author_session_id: "", body: "Stage update", artifact_version_ids: [], created_at: 123, card: null }])),
  http.get("/v1/creative/runs/:id/:resource", () => HttpResponse.json([])),
  http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [] })),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => { server.resetHandlers(); window.history.replaceState({}, "", "/creative/"); });
function open(url = "/creative/") {
  works = [work(7), work(8, "music"), work(9, "music", "accepted"), work(10, "film", "closing")];
  unread = 2; unreadError = false; reads = []; listURLs = [];
  window.history.replaceState({}, "", url);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return { client, ...render(<QueryClientProvider client={client}><App /></QueryClientProvider>) };
}
it("defaults to global Active, labels duplicate names and opens authoritative project context", async () => {
  open(); const user = userEvent.setup();
  expect(await screen.findByRole("heading", { name: "Active works" })).toBeVisible();
  const cards = await screen.findAllByRole("button", { name: /Opening/ });
  expect(cards).toHaveLength(3);
  expect(cards[0]).toHaveTextContent("Film studio");
  expect(cards[1]).toHaveTextContent("Music studio");
  expect(cards[2]).toHaveTextContent("Closure incomplete");
  expect(listURLs.every(url => !new URL(url).searchParams.has("project_id"))).toBe(true);
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  await user.click(cards[1]);
  expect(await screen.findByRole("heading", { name: "Opening" })).toBeVisible();
  expect(screen.getByLabelText("Current work")).toHaveTextContent("Music studio");
  await user.click(screen.getByRole("button", { name: "All works" }));
  expect(window.location.search).toBe("");
  await user.click(screen.getByRole("button", { name: /Opening.*Film studio.*Completed/ }));
  expect(await screen.findByLabelText("Current work")).toHaveTextContent("Film studio");
});
it("restores Completed on reload and popstate and keeps accepted works out of Active", async () => {
  open("/creative/?view=completed"); const user = userEvent.setup();
  expect(await screen.findByRole("heading", { name: "Completed works" })).toBeVisible();
  expect(await screen.findAllByRole("button", { name: /Opening/ })).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: /Opening/ }));
  expect(await screen.findByText(/Work accepted/)).toBeVisible();
  act(() => { window.history.replaceState({}, "", "/creative/?view=completed"); window.dispatchEvent(new PopStateEvent("popstate")); });
  expect(await screen.findByRole("heading", { name: "Completed works" })).toBeVisible();
  await user.click(screen.getByRole("tab", { name: "Active" }));
  expect(await screen.findAllByRole("button", { name: /Opening/ })).toHaveLength(3);
});
it("opens deep links using the run project even when the legacy project parameter is wrong", async () => {
  open("/creative/runs/8?project=film");
  expect(await screen.findByRole("heading", { name: "Opening" })).toBeVisible();
  expect(screen.getByLabelText("Current work")).toHaveTextContent("Music studio");
});
it("polls unopened work unread without marking read, shows failures honestly and updates after reading a stage", async () => {
  const { client } = open();
  await screen.findAllByLabelText("2 unread messages");
  expect(reads).toEqual([]);
  unread = 5;
  await screen.findAllByLabelText("5 unread messages", {}, { timeout: 3500 });
  expect(reads).toEqual([]);
  unreadError = true;
  await screen.findAllByText("Unread unavailable", {}, { timeout: 3500 });
  expect(screen.queryByLabelText("5 unread messages")).not.toBeInTheDocument();
  unreadError = false;
  works[0].stages = [{ id: 11, run_id: 7, position: 0, name: "Draft", description: "", expected_artifacts: "", repeated: false, approval: "stage", status: "in_progress", progress: 0, progress_note: "", review_revision: 0, artifact_version_ids: [], items: [] }];
  await userEvent.click(screen.getAllByRole("button", { name: /Opening/ })[0]);
  await userEvent.click(await screen.findByRole("button", { name: /1. Draft/ }));
  await waitFor(() => expect(reads).toContainEqual({ stage_id: 11, through_entry_id: 41 }));
  await userEvent.click(screen.getByRole("button", { name: "All works" }));
  await act(() => client.invalidateQueries({ queryKey: ["creative", "feed-unread"] }));
  expect(screen.queryByLabelText(/unread messages/)).not.toBeInTheDocument();
});
it("distinguishes list loading, failure and empty Active while retaining navigation", async () => {
  let release!: () => void;
  server.use(http.get("/v1/creative/runs", async () => { await new Promise<void>(resolve => { release = resolve; }); return HttpResponse.json([]); }));
  const { client } = open();
  expect(await screen.findByText("Loading works…")).toBeVisible();
  expect(screen.queryByText("No active work.")).not.toBeInTheDocument();
  await act(async () => release());
  expect(await screen.findByText("No active work.")).toBeVisible();
  expect(screen.getByRole("button", { name: "New work" })).toBeEnabled();
  expect(screen.getByRole("tab", { name: "Completed" })).toBeEnabled();
  server.use(http.get("/v1/creative/runs", () => HttpResponse.json({ error: { message: "Works unavailable" } }, { status: 503 })));
  await act(() => client.invalidateQueries({ queryKey: ["creative", "runs"] }));
  expect(await screen.findByText("Works unavailable")).toBeVisible();
  expect(screen.queryByText("No active work.")).not.toBeInTheDocument();
});
it("requires explicit project selection from overview and navigates to the created project's work", async () => {
  let created: unknown;
  server.use(http.post("/v1/creative/runs", async ({ request }) => {
    created = await request.json(); const run = { ...work(12, "music"), status: "pending", ...(created as object) }; works.push(run as Run); return HttpResponse.json(run);
  }));
  open("/creative/?project=film"); const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "New work" }));
  expect(screen.getByLabelText("Project")).toHaveValue("");
  await user.type(screen.getByLabelText("Work name"), "Music intro");
  await user.type(screen.getByLabelText("Creative brief"), "Compose an intro");
  await user.selectOptions(screen.getByLabelText("Pipeline"), "3");
  expect(screen.getByRole("button", { name: "Create work" })).toBeDisabled();
  await user.selectOptions(screen.getByLabelText("Project"), "music");
  await user.click(screen.getByRole("button", { name: "Create work" }));
  await waitFor(() => expect(created).toEqual({ project_id: "music", name: "Music intro", brief: "Compose an intro", pipeline_id: 3 }));
  expect(await screen.findByLabelText("Current work")).toHaveTextContent("Music studio");
  expect(window.location.search).toBe("?project=music&run=12");
});
it("initializes creation from an opened run, including a reloaded New work URL", async () => {
  const view = open("/creative/runs/8");
  await screen.findByLabelText("Current work");
  await userEvent.click(screen.getByRole("button", { name: "New work" }));
  expect(screen.getByLabelText("Project")).toHaveValue("music");
  const url = window.location.pathname + window.location.search;
  view.unmount();
  open(url);
  expect(await screen.findByRole("heading", { name: "New creative work" })).toBeVisible();
  await waitFor(() => expect(screen.getByLabelText("Project")).toHaveValue("music"));
});
