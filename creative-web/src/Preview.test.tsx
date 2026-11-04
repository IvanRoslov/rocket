import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it } from "vitest";
import { Preview } from "./Preview";
const server = setupServer(
  http.get("/v1/creative/previews/setup", () =>
    HttpResponse.json({
      configured: false,
      message: "Set preview_public_url to a reachable preview origin.",
      allowed_ports: [],
    }),
  ),
  http.get("/v1/creative/previews", ({ request }) =>
    HttpResponse.json(
      new URL(request.url).searchParams.get("stage_id") === "4"
        ? [
            {
              id: "token",
              run_id: 7,
              stage_id: 4,
              port: 5173,
              base_path: "/p/token/",
              status: "ready",
              url: "https://preview.test/p/token/",
            },
          ]
        : [],
    ),
  ),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
function open(stage?: number) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Preview scope={{ run_id: 7, stage_id: stage }} />
    </QueryClientProvider>,
  );
}
it("uses identical access URL for embedded preview and separate tab with restricted sandbox", async () => {
  open(4);
  expect(
    await screen.findByRole("link", { name: "Open preview in new tab" }),
  ).toHaveAttribute("href", "https://preview.test/p/token/");
  expect(screen.getByTitle("Interactive landing preview")).toHaveAttribute(
    "src",
    "https://preview.test/p/token/",
  );
  expect(screen.getByTitle("Interactive landing preview")).toHaveAttribute(
    "sandbox",
    "allow-scripts allow-forms allow-same-origin",
  );
});
it("shows actionable backend setup when preview is unavailable", async () => {
  open();
  expect(
    await screen.findByText(
      "Set preview_public_url to a reachable preview origin.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.queryByTitle("Interactive landing preview"),
  ).not.toBeInTheDocument();
});
