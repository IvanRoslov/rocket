import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { App } from "./App";
const created: unknown[] = [];
const server = setupServer(
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
  http.get("/v1/creative/runs/7/messages", () => HttpResponse.json([])),
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
it("creates work with the chosen pipeline and brief, then clears run scope on project switch", async () => {
  open();
  const user = userEvent.setup();
  await screen.findByRole("option", { name: "Film" });
  await user.click(screen.getByRole("button", { name: "New work" }));
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
  await user.selectOptions(screen.getByLabelText("Project"), "other");
  expect(
    screen.queryByRole("heading", { name: "Opening" }),
  ).not.toBeInTheDocument();
  expect(window.location.search).toBe("?project=other");
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

it("keeps the catalog and unsaved editor draft when switching projects", async () => {
  const hints: (string | null)[] = [];
  server.use(http.get("/v1/creative/pipelines", ({ request }) => {
    hints.push(new URL(request.url).searchParams.get("project_id"));
    return HttpResponse.json([{ id: 3, name: "Film process", stages: [] }]);
  }));
  open();
  const user = userEvent.setup();
  await screen.findByRole("option", { name: "Film" });
  await user.click(screen.getByRole("button", { name: "Pipelines" }));
  await user.click(await screen.findByRole("button", { name: "Film process" }));
  await user.type(screen.getByLabelText("Pipeline name"), " draft");
  await user.selectOptions(screen.getByLabelText("Project"), "other");
  expect(screen.getByLabelText("Pipeline name")).toHaveValue("Film process draft");
  expect(screen.getByRole("button", { name: "Film process" })).toBeInTheDocument();
  expect(hints.length).toBeGreaterThan(0);
  expect(hints.every((hint) => hint === null)).toBe(true);
  await user.click(screen.getByRole("button", { name: "Back to workspace" }));
  await user.click(screen.getByRole("button", { name: "New work" }));
  await user.selectOptions(screen.getByLabelText("Pipeline"), "3");
  expect(screen.getByLabelText("Pipeline")).toHaveValue("3");
});
