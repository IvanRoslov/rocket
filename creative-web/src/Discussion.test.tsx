import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { Discussion } from "./Discussion";
const posted: unknown[] = [];
const server = setupServer(
  http.get("/v1/creative/runs/7/messages", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/sessions", () =>
    HttpResponse.json([{ id: "creative-7", tmux_name: "creative-7", kind: "orchestrator", agent: "codex", state: "running" }]),
  ),
  http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [] })),
  http.post("/v1/creative/runs/7/messages", async ({ request }) => {
    posted.push(await request.json());
    return HttpResponse.json({ id: 1 });
  }),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
it("keeps a draft bound to the stage and artifact version originally shown when selection changes", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = (stage: number, version: number, label: string) => (
    <QueryClientProvider client={client}>
      <Discussion
        context={{
          run_id: 7,
          stage_id: stage,
          label,
          artifact_version_ids: [version],
        }}
        orchestrator="creative-7"
        ready
      />
    </QueryClientProvider>
  );
  const view = render(
    tree(4, 11, "Film / Scenes / Opening · Opening scene version 1"),
  );
  const user = userEvent.setup();
  await user.type(
    screen.getByLabelText("Message or question"),
    "Make the opening warmer",
  );
  view.rerender(tree(5, 12, "Film / Edit · Final cut version 2"));
  expect(
    screen.getByText(/Draft stays with Film \/ Scenes \/ Opening/),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() =>
    expect(posted).toEqual([
      {
        stage_id: 4,
        artifact_version_ids: [11],
        body: "Make the opening warmer",
      },
    ]),
  );
});
it("uses the existing human question answer action for an explicit Go choice", async () => {
  const answers: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7/questions", () =>
      HttpResponse.json([
        {
          id: 21,
          title: "Generate selected scene?",
          body: "One model call with these inputs. Estimated cost $2; provider estimate may vary. Wait for Go.",
          status: "open",
          options: ["Go", "Revise"],
          your_turn: true,
          artifact_version_ids: [],
          messages: [],
        },
      ]),
    ),
    http.post("/v1/questions/21/answer", async ({ request }) => {
      answers.push(await request.json());
      return HttpResponse.json({ id: 21, status: "resolved" });
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Discussion
        context={{
          run_id: 7,
          stage_id: 4,
          label: "Opening scene",
          artifact_version_ids: [],
        }}
        orchestrator="creative-7"
        ready
      />
    </QueryClientProvider>,
  );
  await userEvent.click(await screen.findByRole("button", { name: "Go" }));
  await waitFor(() => expect(answers).toEqual([{ choose: 1 }]));
});
it("launches the dedicated creative orchestrator with an existing enabled profile", async () => {
  const launches: unknown[] = [];
  server.use(
    http.get("/v1/model-profiles", () =>
      HttpResponse.json({
        profiles: [
          { name: "director", enabled: true, model: "configured-model" },
        ],
      }),
    ),
    http.post("/v1/creative/runs/7/sessions", async ({ request }) => {
      launches.push(await request.json());
      return HttpResponse.json({ id: "creative-7", state: "running" });
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Discussion
        context={{ run_id: 7, label: "Film", artifact_version_ids: [] }}
        orchestrator=""
        ready
      />
    </QueryClientProvider>,
  );
  await screen.findByRole("option", { name: "director · configured-model" });
  await userEvent.selectOptions(
    screen.getByLabelText("Model profile"),
    "director",
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Start creative orchestrator" }),
  );
  await waitFor(() => expect(launches).toEqual([{ profile: "director" }]));
});

const accessSessions = [
  { id: "orch /7?", tmux_name: "film-director", kind: "orchestrator", agent: "codex",
    state: "running", activity: "ready", profile: "director", model: "gpt-6", effort: "high" },
  { id: "worker /7#", tmux_name: "film-editor", kind: "worker", agent: "claude-code",
    state: "running", activity: "blocked", model: "opus", effort: "medium" },
];
function renderAccess() {
  server.use(http.get("/v1/creative/runs/7/sessions", () => HttpResponse.json(accessSessions)));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (run = 7, orchestrator = "orch /7?") => (
    <QueryClientProvider client={client}>
      <Discussion context={{ run_id: run, label: "Film", artifact_version_ids: [] }}
        orchestrator={orchestrator} ready />
    </QueryClientProvider>
  );
  return { ...render(tree()), tree };
}
it("shows actual run session identities and models with safe encoded root access links", async () => {
  renderAccess();
  const orch = (await screen.findByText("film-director")).closest<HTMLElement>(".session-rail__orch")!;
  const worker = screen.getByText("film-editor").closest<HTMLElement>(".session-rail__worker")!;
  expect(within(orch).getByText("Orchestrator")).toBeInTheDocument();
  expect(screen.getByText("Workers")).toBeInTheDocument();
  expect(within(orch).getByText("codex")).toBeInTheDocument();
  expect(within(orch).getByText("director (gpt-6, high)")).toBeInTheDocument();
  expect(within(orch).getByText("ready")).toBeInTheDocument();
  expect(within(worker).getByText("claude-code")).toBeInTheDocument();
  expect(within(worker).getByText("opus, medium")).toBeInTheDocument();
  expect(within(worker).getByText("blocked")).toBeInTheDocument();
  for (const [card, term, chat] of [
    [orch, "/term/orch%20%2F7%3F", "/chat/orch%20%2F7%3F"],
    [worker, "/term/worker%20%2F7%23", "/chat/worker%20%2F7%23"],
  ] as const) {
    expect(within(card).getByRole("link", { name: /term/ })).toHaveAttribute("href", term);
    expect(within(card).getByRole("link", { name: /chat/ })).toHaveAttribute("href", chat);
    for (const link of within(card).getAllByRole("link")) {
      expect(link).toHaveAttribute("target", "_blank");
      expect(link).toHaveAttribute("rel", "noopener noreferrer");
    }
  }
  expect(screen.queryByRole("button", { name: /kill|delete|restore/i })).not.toBeInTheDocument();
  expect(screen.queryByText(/PR #|PR —/)).not.toBeInTheDocument();
  expect(screen.getByLabelText("Message or question")).toBeInTheDocument();
});
it("confirms attach only after clipboard success and clears stale confirmation on failure", async () => {
  const user = userEvent.setup();
  let resolveCopy!: () => void;
  const write = vi.spyOn(navigator.clipboard, "writeText")
    .mockImplementationOnce(() => new Promise<void>((resolve) => { resolveCopy = resolve; }))
    .mockRejectedValueOnce(new Error("denied"))
    .mockResolvedValueOnce(undefined);
  renderAccess();
  const orch = (await screen.findByText("film-director")).closest<HTMLElement>(".session-rail__orch")!;
  const worker = screen.getByText("film-editor").closest<HTMLElement>(".session-rail__worker")!;
  await user.click(within(orch).getByRole("button", { name: /attach/ }));
  expect(write).toHaveBeenLastCalledWith("rocket attach film-director");
  expect(screen.queryByText(/copied:/)).not.toBeInTheDocument();
  resolveCopy();
  await screen.findByText("copied: rocket attach film-director");
  await user.click(within(orch).getByRole("button", { name: /attach/ }));
  await waitFor(() => expect(screen.queryByText(/copied:/)).not.toBeInTheDocument());
  await user.click(within(worker).getByRole("button", { name: /attach/ }));
  expect(write).toHaveBeenLastCalledWith("rocket attach film-editor");
  await screen.findByText("copied: rocket attach film-editor");
  write.mockRestore();
});
it("replaces session access cards when the selected run changes", async () => {
  server.use(
    http.get("/v1/creative/runs/8/sessions", () => HttpResponse.json([
      { ...accessSessions[0], id: "next-orch", tmux_name: "next-director" },
    ])),
    http.get("/v1/creative/runs/8/messages", () => HttpResponse.json([])),
    http.get("/v1/creative/runs/8/questions", () => HttpResponse.json([])),
  );
  const view = renderAccess();
  await screen.findByText("film-director");
  view.rerender(view.tree(8, "next-orch"));
  await screen.findByText("next-director");
  expect(screen.queryByText("film-director")).not.toBeInTheDocument();
  expect(screen.queryByText("film-editor")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: /term/ })).toHaveAttribute("href", "/term/next-orch");
});
