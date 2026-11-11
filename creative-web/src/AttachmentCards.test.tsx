import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { AttachmentCards } from "./AttachmentCards";
const server = setupServer(
  http.get("/v1/creative/artifact-versions/:id", ({ params }) => HttpResponse.json({ id: Number(params.id), artifact_id: Number(params.id), version_number: Number(params.id), filename: "scene.png", mime: params.id === "2" ? "text/html" : "image/png", size: 100, content_url: `/content/${params.id}`, download_url: `/download/${params.id}` })),
  http.get("/v1/creative/artifacts/:id", ({ params }) => HttpResponse.json({ artifact: { id: Number(params.id), name: `Scene ${params.id}`, item_id: 5 }, versions: [] })),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
function open(ids = [1, 2, 3, 4, 5]) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <AttachmentCards ids={ids} items={[{ id: 5, name: "Opening" }]} />
  </QueryClientProvider>);
}
it("shows name, item, version and previews with three cards before expanding", async () => {
  open();
  const link = await screen.findByRole("link", { name: /Scene 1/ });
  expect(link).toHaveTextContent("Opening");
  expect(link).toHaveTextContent("version 1");
  expect(within(link).getByRole("img")).toHaveAttribute("src", "/content/1");
  expect(screen.queryByRole("link", { name: /Scene 4/ })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "+2 more" }));
  expect(await screen.findByRole("link", { name: /Scene 5/ })).toBeVisible();
  await userEvent.click(link);
  expect(screen.getByRole("link", { name: "Download scene.png" })).toHaveAttribute("href", "/download/1");
});
it("keeps HTML in isolated viewing", async () => {
  open([2]);
  expect(await screen.findByRole("link", { name: /Scene 2/ })).toHaveAttribute("href", "/v1/creative/artifact-versions/2/view");
  expect(screen.getByRole("link", { name: /Scene 2/ })).toHaveAttribute("target", "_blank");
});
it("keeps failed references visible without hiding the message", async () => {
  server.use(http.get("/v1/creative/artifact-versions/:id", () => HttpResponse.json({ error: { message: "Missing", code: "not_found" } }, { status: 404 })));
  open([1]);
  expect(await screen.findByText("Referenced version is unavailable")).toBeVisible();
});
