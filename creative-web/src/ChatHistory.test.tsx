import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ChatHistory } from "./ChatHistory";
import type { FeedEntry } from "./types";
const entries = [1, 2, 3].map(id => ({ id, author: "orchestrator" } as FeedEntry));
afterEach(() => vi.restoreAllMocks());
it("opens at the first unread above the visible area using the existing unread snapshot", async () => {
  const client = new QueryClient();
  client.setQueryData(["creative", "feed-unread", 7], { stages: [{ stage_id: 4, unread: 2 }] });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    return { top: this.id === "feed-entry-2" ? -100 : 0 } as DOMRect;
  });
  render(<QueryClientProvider client={client}><ChatHistory entries={entries} scope={{ run_id: 7, stage_id: 4 }} active>
    {entries.map(e => <article key={e.id} id={`feed-entry-${e.id}`} tabIndex={-1}>Message {e.id}</article>)}
  </ChatHistory></QueryClientProvider>);
  expect(document.activeElement?.id).toBe("feed-entry-2");
});
it("waits until a hidden chat becomes visible and follows late content sizing only at bottom", async () => {
  let resize!: ResizeObserverCallback;
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: ResizeObserverCallback) { resize = callback; }
    observe() {} disconnect() {}
  });
  const client = new QueryClient();
  const tree = (active: boolean) => <QueryClientProvider client={client}><ChatHistory entries={entries} scope={{ run_id: 7 }} active={active}><p>History</p></ChatHistory></QueryClientProvider>;
  const view = render(tree(false));
  const history = screen.getByRole("region", { name: "Chat history" });
  Object.defineProperties(history, { scrollHeight: { configurable: true, value: 1000 }, clientHeight: { configurable: true, value: 200 } });
  expect(history.scrollTop).toBe(0);
  view.rerender(tree(true));
  expect(history.scrollTop).toBe(1000);
  Object.defineProperty(history, "scrollHeight", { configurable: true, value: 1200 });
  act(() => resize([], {} as ResizeObserver));
  expect(history.scrollTop).toBe(1200);
  history.scrollTop = 100; fireEvent.scroll(history);
  Object.defineProperty(history, "scrollHeight", { configurable: true, value: 1400 });
  act(() => resize([], {} as ResizeObserver));
  expect(history.scrollTop).toBe(100);
  vi.unstubAllGlobals();
});
