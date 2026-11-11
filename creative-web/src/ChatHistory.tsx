import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { FeedEntry, Scope } from "./types";

export function scrollToEntry(container: HTMLElement, id: number) {
  const entry = container.querySelector<HTMLElement>(`#feed-entry-${id}`);
  if (!entry) return;
  container.scrollTop += entry.getBoundingClientRect().top - container.getBoundingClientRect().top - 16;
  entry.focus({ preventScroll: true });
  container.dispatchEvent(new Event("scroll"));
}

export function ChatHistory({ entries, active, children, unreadReady = true, unreadCount = 0 }: {
  entries: FeedEntry[]; scope: Scope; active: boolean; children: ReactNode; unreadReady?: boolean; unreadCount?: number;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const initialized = useRef(false);
  const seen = useRef(0);
  const [newCount, setNewCount] = useState(0);
  const lastScrollTop = useRef(0);
  const anchor = useRef<{ id: string; offset: number } | undefined>(undefined);
  function rememberAnchor() {
    const el = viewport.current;
    if (!el) return;
    lastScrollTop.current = el.scrollTop;
    const top = el.getBoundingClientRect().top;
    const entry = Array.from(el.querySelectorAll<HTMLElement>("article[id]"))
      .find(node => node.getBoundingClientRect().bottom > top);
    anchor.current = entry ? { id: entry.id, offset: entry.getBoundingClientRect().top - top } : undefined;
  }
  function preserveAnchor() {
    const el = viewport.current;
    const saved = anchor.current;
    const entry = saved && el?.querySelector<HTMLElement>(`#${saved.id}`);
    if (el && entry) el.scrollTop += entry.getBoundingClientRect().top - el.getBoundingClientRect().top - saved!.offset;
    rememberAnchor();
  }
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
    if (!el || !active || !unreadReady || !entries.length) return;
    if (!initialized.current) {
      initialized.current = true;
      bottom();
      const unread = unreadCount ? entries.filter(e => e.author === "orchestrator").slice(-unreadCount)[0] : undefined;
      const target = unread && el.querySelector<HTMLElement>(`#feed-entry-${unread.id}`);
      if (target && target.getBoundingClientRect().top < el.getBoundingClientRect().top) {
        scrollToEntry(el, unread.id);
        atBottom.current = false;
      }
    } else if (atBottom.current) bottom();
    else { preserveAnchor(); setNewCount(entries.filter(e => e.id > seen.current).length); }
    rememberAnchor();
  }, [entries, active, latest, unreadReady, unreadCount]);
  useLayoutEffect(() => {
    if (typeof ResizeObserver === "undefined" || !content.current) return;
    const observer = new ResizeObserver(() => {
      if (!active || !initialized.current) return;
      if (atBottom.current && viewport.current) viewport.current.scrollTop = viewport.current.scrollHeight;
      else preserveAnchor();
    });
    observer.observe(content.current);
    return () => observer.disconnect();
  }, [active]);
  return <div className="chat-history-wrap">
    <div className="chat-history" ref={viewport} role="region" aria-label="Chat history" tabIndex={0}
      onScroll={() => {
        const el = viewport.current!;
        if (lastScrollTop.current === el.scrollTop) return;
        rememberAnchor();
        atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
        if (atBottom.current) { seen.current = latest; setNewCount(0); }
      }}>
      <div ref={content}>{children}</div>
    </div>
    {newCount > 0 && <button className="chat-new" onClick={bottom}>{newCount} new ↓</button>}
  </div>;
}
