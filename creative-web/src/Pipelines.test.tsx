import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, it, expect } from "vitest";
import { Pipelines } from "./Pipelines";
const saved: unknown[] = [];
const server = setupServer(
  http.post("/v1/creative/pipelines", async ({ request }) => {
    const body = await request.json();
    saved.push(body);
    return HttpResponse.json({ id: 9, ...(body as object) });
  }),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
it("saves ordered repeated stages and approval settings as a template", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Pipelines pipelines={[]} onClose={() => {}} />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Pipeline name"), "Film process");
  await user.type(screen.getByLabelText("Stage 1 name"), "Scenes");
  await user.type(screen.getByLabelText("Stage 1 work"), "Create each scene");
  await user.type(
    screen.getByLabelText("Stage 1 expected artifacts"),
    "One clip",
  );
  await user.click(screen.getByLabelText("Stage 1 repeated items"));
  await user.selectOptions(screen.getByLabelText("Stage 1 approval"), "item");
  await user.click(screen.getByRole("button", { name: "Add stage" }));
  await user.type(screen.getByLabelText("Stage 2 name"), "Assemble");
  await user.click(screen.getByRole("button", { name: "Move stage 2 up" }));
  await user.click(screen.getByRole("button", { name: "Save pipeline" }));
  await waitFor(() =>
    expect(saved).toEqual([
      {
        name: "Film process",
        stages: [
          {
            name: "Assemble",
            description: "",
            expected_artifacts: "",
            repeated: false,
            approval: "stage",
          },
          {
            name: "Scenes",
            description: "Create each scene",
            expected_artifacts: "One clip",
            repeated: true,
            approval: "item",
          },
        ],
      },
    ]),
  );
});
