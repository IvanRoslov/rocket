import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { App } from "./App";
const created: unknown[] = [];
const server = setupServer(
  http.get("/v1/creative/runs/:run/feed/unread", ({ params }) => HttpResponse.json({ run_id: Number(params.run), total: 0, run_feed: 0, stages: [] })),
  http.post("/v1/creative/runs/:run/feed/read", async ({ request }) => HttpResponse.json(await request.json())),
  http.get("/v1/projects", () =>
    HttpResponse.json([
      { id: "film", name: "Film" },
      { id: "other", name: "Other" },
    ]),
  ),
  http.get("/v1/creative/pipelines", () =>
    HttpResponse.json([{ id: 3, name: "Film process", stages: [
      { name: "Draft", description: "", expected_artifacts: "", repeated: false, approval: "stage" },
    ] }]),
  ),
  http.get("/v1/creative/runs", () => HttpResponse.json([])),
  http.post("/v1/creative/runs", async ({ request }) => {
    const body = await request.json();
    created.push(body);
    return HttpResponse.json({
      id: 7,
      ...(body as object),
      pipeline_name: "Film process",
      stages: [],
      status: "pending",
      orchestrator_session_id: "",
    });
  }),
  http.get("/v1/creative/runs/7", () =>
    HttpResponse.json({
      id: 7,
      project_id: "film",
      name: "Opening",
      brief: "A short film",
      pipeline_name: "Film process",
      stages: [],
      status: "pending",
      orchestrator_session_id: "",
    }),
  ),
  http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([])),
  http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [] })),
  http.get("/v1/creative/runs/7/artifacts", () => HttpResponse.json([])),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  window.history.replaceState({}, "", "/creative/");
  server.resetHandlers();
});
afterAll(() => server.close());
function open() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <App />
    </QueryClientProvider>,
  );
}
it("creates work with the chosen project, pipeline and brief, then returns to All works", async () => {
  open();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "New work" }));
  await user.selectOptions(await screen.findByLabelText("Project"), "film");
  await user.type(screen.getByLabelText("Work name"), "Opening");
  await user.type(screen.getByLabelText("Creative brief"), "A short film");
  await user.selectOptions(screen.getByLabelText("Pipeline"), "3");
  await user.click(screen.getByRole("button", { name: "Create work" }));
  await waitFor(() =>
    expect(created).toEqual([
      {
        project_id: "film",
        pipeline_id: 3,
        name: "Opening",
        brief: "A short film",
      },
    ]),
  );
  expect(
    await screen.findByRole("heading", { name: "Opening" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "All works" }));
  expect(await screen.findByRole("heading", { name: "Active works" })).toBeVisible();
  expect(window.location.search).toBe("");
});

it("opens and edits the shared catalog without any projects", async () => {
  server.use(http.get("/v1/projects", () => HttpResponse.json([])));
  let saved: unknown;
  server.use(http.put("/v1/creative/pipelines/3", async ({ request }) => {
    saved = await request.json();
    return HttpResponse.json({ id: 3, ...(saved as object) });
  }));
  open();
  const user = userEvent.setup();
  await screen.findByText("Create a Rocket project to begin.");
  await user.click(screen.getByRole("button", { name: "Pipelines" }));
  await user.click(await screen.findByRole("button", { name: "Film process" }));
  await user.clear(screen.getByLabelText("Pipeline name"));
  await user.type(screen.getByLabelText("Pipeline name"), "Shared process");
  await user.click(screen.getByRole("button", { name: "Save pipeline" }));
  await waitFor(() => expect(saved).toEqual({
    name: "Shared process",
    stages: [{ name: "Draft", description: "", expected_artifacts: "", repeated: false, approval: "stage" }],
  }));
});

it("creates a template without any projects", async () => {
  server.use(http.get("/v1/projects", () => HttpResponse.json([])));
  let saved: unknown;
  server.use(http.post("/v1/creative/pipelines", async ({ request }) => {
    saved = await request.json();
    return HttpResponse.json({ id: 4, ...(saved as object) });
  }));
  open();
  const user = userEvent.setup();
  await screen.findByText("Create a Rocket project to begin.");
  await user.click(screen.getByRole("button", { name: "Pipelines" }));
  await user.type(screen.getByLabelText("Pipeline name"), "New shared process");
  await user.type(screen.getByLabelText("Stage 1 name"), "Draft");
  await user.click(screen.getByRole("button", { name: "Save pipeline" }));
  await waitFor(() => expect(saved).toEqual({
    name: "New shared process",
    stages: [{ name: "Draft", description: "", expected_artifacts: "", repeated: false, approval: "stage" }],
  }));
});

it("keeps the catalog global and its unsaved editor draft while editing", async () => {
  const hints: (string | null)[] = [];
  server.use(http.get("/v1/creative/pipelines", ({ request }) => {
    hints.push(new URL(request.url).searchParams.get("project_id"));
    return HttpResponse.json([{ id: 3, name: "Film process", stages: [] }]);
  }));
  open();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Pipelines" }));
  await user.click(await screen.findByRole("button", { name: "Film process" }));
  await user.type(screen.getByLabelText("Pipeline name"), " draft");
  expect(screen.getByLabelText("Pipeline name")).toHaveValue("Film process draft");
  expect(screen.getByRole("button", { name: "Film process" })).toBeInTheDocument();
  expect(hints.length).toBeGreaterThan(0);
  expect(hints.every((hint) => hint === null)).toBe(true);
  await user.click(screen.getByRole("button", { name: "Back to workspace" }));
  await user.click(screen.getByRole("button", { name: "New work" }));
  await user.selectOptions(screen.getByLabelText("Pipeline"), "3");
  expect(screen.getByLabelText("Pipeline")).toHaveValue("3");
});

// These integration cases catch accidental panel unmounts, scope resets and
// approval becoming available from data loaded only in a hidden panel.
function workspaceFixture(status = "awaiting_approval") {
  const work = {
    description: "Plan the opening scene", expected_artifacts: "Scene image",
    status, progress: 80, progress_note: "Ready for a look",
    review_revision: 3, artifact_version_ids: [12],
  };
  const item = { ...work, id: 5, stage_id: 4, run_id: 7, position: 0, name: "Opening scene" };
  const stage = { ...work, id: 4, run_id: 7, position: 0, name: "Scenes", status: "in_progress", repeated: true, approval: "item", items: [item] };
  const artifact = { id: 9, run_id: 7, stage_id: 4, item_id: 5, name: "Scene image", kind: "result", current_version_id: 12 };
  const versions = [11, 12].map((id, i) => ({
    id, artifact_id: 9, version_number: i + 1, filename: `scene-${id}.png`,
    mime: "image/png", size: 100, content_url: `/content/${id}`, download_url: `/download/${id}`,
  }));
  const actions: unknown[] = [], messages: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json({
      id: 7, project_id: "film", pipeline_id: 3, pipeline_name: "Film process", name: "Opening",
      brief: "A short film", status, orchestrator_session_id: "creative-7", stages: [stage],
    })),
    http.get("/v1/creative/runs/7/artifacts", ({ request }) => {
      const scope = new URL(request.url).searchParams;
      return HttpResponse.json(scope.get("item_id") === "5" ? [artifact] : []);
    }),
    http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json([
      { id: "creative-7", tmux_name: "creative-7", kind: "orchestrator", agent: "codex", state: "running" },
    ])),
    http.get("/v1/creative/artifact-versions/:id", ({ params }) => HttpResponse.json(versions.find((v) => v.id === Number(params.id)))),
    http.get("/v1/creative/artifacts/9", () => HttpResponse.json({ artifact, versions })),
    http.post("/v1/creative/runs/7/stages/4/items/5/actions", async ({ request }) => {
      actions.push(await request.json());
      return HttpResponse.json({});
    }),
    http.post("/v1/creative/runs/7/feed", async ({ request }) => {
      messages.push(await request.json());
      return HttpResponse.json({});
    }),
  );
  window.history.replaceState({}, "", "/creative/?project=film&run=7&stage=4&item=5");
  return { actions, messages };
}

it("shows the sole highlighted review above Sessions in the sidebar and approves from discussion", async () => {
  const { actions } = workspaceFixture();
  open();
  const user = userEvent.setup();
  const tabs = await screen.findByRole("tablist", { name: "Workspace" });
  expect(within(tabs).getAllByRole("tab")).toHaveLength(2);
  const card = screen.getByRole("region", { name: "Review Opening scene" });
  const sidebar = screen.getByRole("complementary", { name: "Review and sessions" });
  expect(sidebar).toContainElement(card);
  expect(screen.getByRole("main")).not.toContainElement(card);
  expect(card).toHaveClass("review-actions--pending");
  expect(card.compareDocumentPosition(within(sidebar).getByRole("heading", { name: "Sessions" })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(within(tabs).getByRole("tab", { name: "Conversation" })).toHaveAttribute("aria-selected", "true");
  expect(await within(card).findByRole("link", { name: "Scene image · version 2" })).toBeVisible();
  expect(screen.getAllByRole("button", { name: "Approve item" })).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Approve item" }));
  await waitFor(() => expect(actions).toEqual([{ action: "approve", review_revision: 3 }]));
  expect(screen.queryByRole("button", { name: "Approve item" })).not.toBeInTheDocument();
});

it("retains an older selected version and draft through tabs and keeps the draft bound on scope navigation", async () => {
  const { messages } = workspaceFixture("in_progress");
  open();
  const user = userEvent.setup();
  await user.click(await screen.findByRole("tab", { name: "Artifacts" }));
  await user.selectOptions(await screen.findByLabelText("Scene image version"), "11");
  await user.click(screen.getByRole("tab", { name: "Conversation" }));
  await waitFor(() => expect(screen.getByText(/About: Opening scene/)).toBeVisible());
  await user.type(screen.getByLabelText("Message"), "Keep this framing");
  await user.click(screen.getByRole("tab", { name: "Artifacts" }));
  expect(screen.getByLabelText("Scene image version")).toHaveValue("11");
  expect(screen.getByRole("img", { name: "scene-11.png" })).toBeVisible();
  await user.click(screen.getByRole("tab", { name: "Conversation" }));
  expect(screen.getByLabelText("Message")).toHaveValue("Keep this framing");
  const sessions = screen.getByRole("complementary", { name: "Run sessions" });
  expect(within(sessions).getByRole("link", { name: /term/ })).toHaveAttribute("href", "/term/creative-7");
  expect(within(sessions).getByRole("link", { name: /chat/ })).toHaveAttribute("href", "/chat/creative-7");
  expect(within(sessions).getByRole("button", { name: "Copy attach command for creative-7" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: /1. Scenes/ }));
  expect(window.location.search).toBe("?project=film&run=7&stage=4");
  expect(screen.getByText(/Draft stays with Opening \/ Scenes \/ Opening scene/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() => expect(messages).toEqual([{ stage_id: 4, item_id: 5, body: "Keep this framing" }]));
});

it("keeps the submitted set independent of browsing history and tab changes", async () => {
  const { actions } = workspaceFixture();
  open();
  const user = userEvent.setup();
  await user.click(await screen.findByRole("tab", { name: "Artifacts" }));
  await user.selectOptions(await screen.findByLabelText("Scene image version"), "11");
  const card = screen.getByRole("region", { name: "Review Opening scene" });
  await user.click(await within(card).findByRole("link", { name: "Scene image · version 2" }));
  expect(within(card).getByRole("img")).toHaveAttribute("src", "/content/12");
  expect(screen.getByLabelText("Scene image version")).toHaveValue("11");
  expect(screen.getByRole("button", { name: "Approve item" })).toBeEnabled();
  await user.click(screen.getByRole("tab", { name: "Conversation" }));
  expect(actions).toEqual([]);
  expect(screen.getByRole("button", { name: "Approve item" })).toBeEnabled();
});

it("keeps heading and progress compact while full work details remain available", async () => {
  workspaceFixture();
  open();
  const user = userEvent.setup();
  expect(await screen.findByRole("heading", { name: "Opening scene" })).toBeVisible();
  expect(screen.getByRole("progressbar")).toHaveAttribute("value", "80");
  const details = screen.getByText("Work details").closest("details");
  expect(details).not.toHaveAttribute("open");
  expect(screen.getByText("Plan the opening scene")).not.toBeVisible();
  await user.click(screen.getByText("Work details"));
  expect(details).toHaveAttribute("open");
  expect(screen.getByText("Plan the opening scene")).toBeVisible();
  expect(screen.getByText("Scene image", { selector: "p" })).toBeVisible();
  expect(screen.getByText("Ready for a look")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "OpeningReady for your review" }));
  expect(await screen.findByRole("heading", { name: "Opening" })).toBeVisible();
  const brief = screen.getAllByText("Creative brief").map((s) => s.closest("details")).filter(Boolean);
  expect(brief).toHaveLength(2);
  brief.forEach((d) => expect(d).not.toHaveAttribute("open"));
});

it("keeps the session rail outside central content and visible on every workspace tab", async () => {
  workspaceFixture();
  open();
  const user = userEvent.setup();
  const rail = await screen.findByRole("complementary", { name: "Run sessions" });
  expect(screen.getByRole("main")).not.toContainElement(rail);
  for (const tab of ["Artifacts", "Conversation"]) {
    await user.click(screen.getByRole("tab", { name: tab }));
    expect(rail).toBeVisible();
    expect(within(rail).getByRole("link", { name: /term/ })).toHaveAttribute("href", "/term/creative-7");
    expect(within(rail).getByRole("button", { name: /Copy attach/ })).toBeEnabled();
  }
});

it("uses saved run worker profiles and isolates unsaved edits when switching runs", async () => {
  const run = (id: number, allowed_profiles: string[]) => ({
    id, project_id: "film", pipeline_id: 3, pipeline_name: "Film process",
    name: `Run ${id}`, brief: "A short film", stages: [], status: "in_progress",
    orchestrator_session_id: `creative-${id}`, allowed_profiles,
  });
  server.use(
    http.get("/v1/creative/runs", () => HttpResponse.json([run(7, ["writer"]), run(8, [])])),
    http.get("/v1/creative/runs/7", () => HttpResponse.json(run(7, ["writer"]))),
    http.get("/v1/creative/runs/8", () => HttpResponse.json(run(8, []))),
    http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [
      { name: "writer", model: "model-w", enabled: true },
    ] })),
    http.get("/v1/creative/runs/8/:resource", () => HttpResponse.json([])),
  );
  window.history.replaceState({}, "", "/creative/?project=film&run=7");
  open();
  const summary = await screen.findByRole("region", { name: "Worker profiles" });
  expect(within(summary).getByText("writer")).toBeVisible();
  await userEvent.click(await screen.findByRole("button", { name: "Edit worker profiles" }));
  await userEvent.click(screen.getByRole("checkbox", { name: /writer/ }));
  await userEvent.click(screen.getByRole("button", { name: "All works" }));
  await userEvent.click(await screen.findByRole("button", { name: /Run 8/ }));
  expect(await screen.findByRole("heading", { name: "Run 8" })).toBeVisible();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.getByText("all profiles")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "All works" }));
  await userEvent.click(await screen.findByRole("button", { name: /Run 7/ }));
  expect(await screen.findByRole("heading", { name: "Run 7" })).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "Edit worker profiles" }));
  expect(screen.getByRole("checkbox", { name: /writer/ })).toBeChecked();
});

function completedFixture(state: "open" | "closing" | "accepted" = "open", error = "") {
  return {
    id: 7, project_id: "film", pipeline_id: 3, pipeline_name: "Film process", name: "Opening", brief: "Saved film", status: "completed",
    closure_state: state, accepted_at: state === "accepted" ? 123 : 0, closure_error: error, orchestrator_session_id: "",
    stages: [{ id: 11, run_id: 7, position: 0, name: "Draft", description: "Saved scene", expected_artifacts: "Clip", repeated: false, approval: "stage", status: "completed", progress: 100, progress_note: "", review_revision: 1, artifact_version_ids: [], items: [] }],
  };
}
it("offers final human acceptance on the last stage and retains its selected readable archive", async () => {
  let run = completedFixture();
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json(run)),
    http.get("/v1/creative/runs", () => HttpResponse.json([run])),
    http.post("/v1/creative/runs/7/accept", () => { run = completedFixture("accepted"); return HttpResponse.json(run); }),
  );
  window.history.replaceState({}, "", "/creative/?project=film&run=7&stage=11");
  open();
  const user = userEvent.setup();
  const accept = await screen.findByRole("button", { name: "Accept all work" });
  expect(screen.getByText(/permanently closes/i)).toBeInTheDocument();
  await user.click(accept);
  expect(await screen.findByText(/Work accepted/)).toBeInTheDocument();
  expect(window.location.search).toContain("stage=11");
  expect(screen.queryByRole("button", { name: /Reopen|Restore creative|Start creative/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox", { name: "Message" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Artifacts" }));
  expect(screen.queryByRole("button", { name: "Upload source" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "All works" }));
  expect(screen.queryByRole("button", { name: /Opening/ })).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Completed" }));
  expect(await screen.findByRole("heading", { name: "Completed works" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Opening/ }));
  expect(await screen.findByText(/Work accepted/)).toBeInTheDocument();
});
it("keeps incomplete closure visible with an intentional retry after failure", async () => {
  let run = completedFixture(); let attempts = 0;
  server.use(
    http.get("/v1/creative/runs/7", () => HttpResponse.json(run)),
    http.get("/v1/creative/runs", () => HttpResponse.json([run])),
    http.post("/v1/creative/runs/7/accept", () => {
      if (++attempts === 1) { run = completedFixture("closing", "Could not stop worker"); return HttpResponse.json({ error: { code: "internal_error", message: "Could not stop worker" } }, { status: 500 }); }
      run = completedFixture("accepted"); return HttpResponse.json(run);
    }),
  );
  window.history.replaceState({}, "", "/creative/?project=film&run=7");
  open(); const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Accept all work" }));
  expect(await screen.findByText(/Closure incomplete/)).toBeInTheDocument();
  expect(screen.queryByText(/Work accepted/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "All works" }));
  await user.click(await screen.findByRole("button", { name: /Opening.*Closure incomplete/ }));
  await user.click(await screen.findByRole("button", { name: "Retry closure" }));
  expect(await screen.findByText(/Work accepted/)).toBeInTheDocument();
});
it("opens an accepted direct link after reload and excludes it from active cards", async () => {
  const run = completedFixture("accepted");
  server.use(http.get("/v1/creative/runs/7", () => HttpResponse.json(run)), http.get("/v1/creative/runs", () => HttpResponse.json([run])));
  window.history.replaceState({}, "", "/creative/runs/7");
  open();
  expect(await screen.findByText(/Work accepted/)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "All works" }));
  expect(screen.queryByRole("button", { name: /Opening/ })).not.toBeInTheDocument();
});
