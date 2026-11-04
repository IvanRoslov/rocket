import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it } from "vitest";
import { Discussion } from "./Discussion";
const posted: unknown[] = [];
const server = setupServer(
  http.get("/v1/creative/runs/7/messages", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/sessions", () =>
    HttpResponse.json([{ id: "creative-7", state: "running" }]),
  ),
  http.get("/v1/model-profiles", () => HttpResponse.json({ profiles: [] })),
  http.post("/v1/creative/runs/7/messages", async ({ request }) => {
    posted.push(await request.json());
    return HttpResponse.json({ id: 1 });
  }),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
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
