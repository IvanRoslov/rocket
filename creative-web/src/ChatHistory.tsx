import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { unreadKey } from "./feed";
import type { FeedEntry, FeedUnread, Scope } from "./types";

export function scrollToEntry(container: HTMLElement, id: number) {
  const entry = container.querySelector<HTMLElement>(`#feed-entry-${id}`);
  if (!entry) return;
  container.scrollTop += entry.getBoundingClientRect().top - container.getBoundingClientRect().top - 16;
  entry.focus({ preventScroll: true });
}

export function ChatHistory({ entries, scope, active, children }: {
  entries: FeedEntry[]; scope: Scope; active: boolean; children: ReactNode;
}) {
  const client = useQueryClient();
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const initialized = useRef(false);
  const seen = useRef(0);
  const [newCount, setNewCount] = useState(0);
  // Snapshot before marking read invalidates the badge count.
  const [initialUnread] = useState(() => {
    const unread = client.getQueryData<FeedUnread>(unreadKey(scope.run_id));
    return scope.stage_id ? unread?.stages.find(s => s.stage_id === scope.stage_id)?.unread : unread?.run_feed;
  });
  const latest = entries.at(-1)?.id ?? 0;
  function bottom() {
    const el = viewport.current;
    if (el) el.scrollTop = el.scrollHeight;
    atBottom.current = true;
    seen.current = latest;
    setNewCount(0);
  }
  useLayoutEffect(() => {
    const el = viewport.current;
    if (!el || !active || !entries.length) return;
    if (!initialized.current) {
      initialized.current = true;
      bottom();
      const unread = initialUnread ? entries.filter(e => e.author === "orchestrator").slice(-initialUnread)[0] : undefined;
      const target = unread && el.querySelector<HTMLElement>(`#feed-entry-${unread.id}`);
      if (target && target.getBoundingClientRect().top < el.getBoundingClientRect().top) {
        scrollToEntry(el, unread.id);
        atBottom.current = false;
      }
    } else if (atBottom.current) bottom();
    else setNewCount(entries.filter(e => e.id > seen.current).length);
  }, [entries, active, latest, initialUnread]);
  useLayoutEffect(() => {
    if (typeof ResizeObserver === "undefined" || !content.current) return;
    const observer = new ResizeObserver(() => {
      if (active && initialized.current && atBottom.current && viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight;
    });
    observer.observe(content.current);
    return () => observer.disconnect();
  }, [active]);
  return <div className="chat-history-wrap">
    <div className="chat-history" ref={viewport} role="region" aria-label="Chat history" tabIndex={0}
      onScroll={() => {
        const el = viewport.current!;
        atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
        if (atBottom.current) { seen.current = latest; setNewCount(0); }
      }}>
      <div ref={content}>{children}</div>
    </div>
    {newCount > 0 && <button className="chat-new" onClick={bottom}>{newCount} new ↓</button>}
  </div>;
}
