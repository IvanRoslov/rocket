import { Button } from "../../web/src/components/Button";
import { useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, base, useCommand } from "./api";
import { FeedReadMarker } from "./FeedReadMarker";
import { FeedEntryView } from "./FeedEntry";
import { ChatHistory, scrollToEntry } from "./ChatHistory";
import { QuestionArchive } from "./QuestionArchive";
import { cacheFeedEntry, useFeed, useUnread } from "./feed";
import type { Context, FeedEntry, Item } from "./types";
import { Empty, ErrorNotice } from "./ui";

export function Discussion({ context, ready, items = [], onNavigate, active = true, stageName }: {
  context: Context;
  stageName?: string;
  orchestrator: string;
  ready: boolean;
  active?: boolean;
  items?: Pick<Item, "id" | "name">[];
  onNavigate?: (stage?: number, item?: number) => void;
}) {
  const selectionKey = `${context.run_id}:${context.stage_id ?? 0}:${context.item_id ?? 0}`;
  const [filteredSelection, setFilteredSelection] = useState<string>();
  const onlyItem = !!context.item_id && filteredSelection === selectionKey;
  const feedScope = { run_id: context.run_id, stage_id: context.stage_id, ...(onlyItem ? { item_id: context.item_id } : {}) };
  const feed = useFeed(feedScope);
  const unread = useUnread(context.run_id);
  const unreadCount = context.stage_id ? unread.data?.stages.find(stage => stage.stage_id === context.stage_id)?.unread : unread.data?.run_feed;
  const [body, setBody] = useState("");
  const [draftContext, setDraftContext] = useState<Context>();
  const command = useCommand();
  const client = useQueryClient();
  const target = draftContext ?? context;
  const changed = target.run_id !== context.run_id || target.stage_id !== context.stage_id || target.item_id !== context.item_id;
  function reset() {
    setBody("");
    setDraftContext(undefined);
    command.reset();
  }
  const [archiveOpen, setArchiveOpen] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    input.style.height = "auto";
    input.style.height = `${Math.min(input.scrollHeight, 104)}px`;
  }, [body, active, archiveOpen]);
  const [jump, setJump] = useState<FeedEntry>();
  const windowRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!jump || jump.run_id !== context.run_id || (jump.stage_id || 0) !== (context.stage_id || 0)) return;
    const element = document.getElementById(`feed-entry-${jump.id}`);
    if (element) {
      const history = windowRef.current?.querySelector<HTMLElement>(".chat-history");
      if (history) scrollToEntry(history, jump.id);
      element.focus({ preventScroll: true });
      setJump(undefined);
    }
  }, [jump, context.run_id, context.stage_id, feed.data]);
  function jumpTo(successor: FeedEntry) {
    setFilteredSelection(undefined);
    setJump(successor);
    onNavigate?.(successor.stage_id || undefined, successor.item_id || undefined);
  }
  const sorted = useMemo(() => [...(feed.data ?? [])].sort((a, b) => a.id - b.id), [feed.data]);
  const pending = sorted.filter(entry => entry.card?.status === "pending");
  const pendingIndex = useRef(0);
  const stageLabel = stageName ?? context.label.split(" / ")[1] ?? context.label;
  const itemLabel = items.find(item => item.id === context.item_id)?.name;
  const targetLabel = changed ? target.label.split(" · ")[0] : target.label.split(" / ").at(-1) ?? target.label;
  return (
    <section className="discussion">
      <div className="chat-window" ref={windowRef}>
        <header className="chat-heading">
          <h2>{context.stage_id ? `Stage chat «${stageLabel}»` : "Run chat"}</h2>
          {context.item_id && <span>{itemLabel ?? `Item ${context.item_id}`}</span>}
          <button type="button" className="chat-archive-toggle" aria-expanded={archiveOpen} onClick={() => setArchiveOpen(!archiveOpen)}>Old questions</button>
          {context.item_id && <label className="feed-filter"><input type="checkbox" checked={onlyItem} onChange={(e) => setFilteredSelection(e.target.checked ? selectionKey : undefined)} />Only this item</label>}
        </header>
        {pending.length > 0 && !archiveOpen && <button className="chat-pending" onClick={() => {
          const history = windowRef.current?.querySelector<HTMLElement>(".chat-history");
          if (history) scrollToEntry(history, pending[pendingIndex.current++ % pending.length].id);
        }}>Awaiting decision: {pending.length} <span>View proposal ↓</span></button>}
        <ErrorNotice error={feed.error} />
        <FeedReadMarker key={`read:${selectionKey}:${onlyItem}`} scope={feedScope} active={active && !archiveOpen}
          loaded={feed.data !== undefined && !unread.isPending} through={sorted.at(-1)?.id ?? 0} updatedAt={feed.dataUpdatedAt} />
        <div className="conversation-history" hidden={archiveOpen}>
          <ChatHistory key={`history:${selectionKey}:${onlyItem}`} entries={sorted} scope={feedScope} active={active && !archiveOpen} unreadReady={!unread.isPending} unreadCount={onlyItem ? 0 : unreadCount}>
            {feed.isPending && <p>Loading conversation…</p>}
            {feed.data?.length === 0 && <Empty>No messages in this feed.</Empty>}
            <ol className="conversation-entries" aria-label="Conversation entries">
              {sorted.map((entry, index) => {
                const date = new Date(entry.created_at * 1000);
                const newDay = !index || date.toDateString() !== new Date(sorted[index - 1].created_at * 1000).toDateString();
                return <li key={entry.id}>
                  {newDay && <div className="chat-date">{date.toDateString() === new Date().toDateString() ? "Today" : date.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}</div>}
                  <FeedEntryView entry={entry} items={items} contextLabel={context.stage_id ? stageLabel : "Run"} onJump={jumpTo} />
                </li>;
              })}
            </ol>
          </ChatHistory>
        </div>
        <div className="chat-archive" hidden={!archiveOpen}>
          <button type="button" className="chat-archive-toggle" onClick={() => setArchiveOpen(false)}>Back to chat</button>
          <QuestionArchive context={context} />
        </div>
        <form hidden={archiveOpen} className="composer question-thread__form" onSubmit={(e) => {
          e.preventDefault();
          if (!body.trim() || command.isPending || (!ready && !draftContext)) return;
          const { run_id, stage_id, item_id } = target;
          command.mutate(async () => {
            const entry = await api.post<FeedEntry>(`${base}/runs/${run_id}/feed`, {
              ...(stage_id ? { stage_id } : {}), ...(item_id ? { item_id } : {}), body,
            });
            await cacheFeedEntry(client, entry);
          }, { onSuccess: reset });
        }}>
          <textarea className="chat-input" aria-label="Message" ref={inputRef} rows={1} value={body} onChange={(e) => {
            if (!draftContext) setDraftContext({ ...context, artifact_version_ids: [...context.artifact_version_ids] });
            setBody(e.target.value);
          }} onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
              e.preventDefault(); e.currentTarget.form?.requestSubmit();
            }
          }} placeholder="Write to the orchestrator…" disabled={command.isPending || (!ready && !draftContext)} />
          <div className="row composer-actions">
            <p className="composer-context">{changed ? `Draft stays with ${targetLabel}` : `About: ${targetLabel}`}</p>
            <Button variant="primary" size="sm" type="submit" disabled={!body.trim() || command.isPending || (!ready && !draftContext)}>Send message</Button>
            {body && <Button variant="secondary" size="sm" type="button" onClick={reset} disabled={command.isPending}>Discard draft</Button>}
            <small>⌘ / Ctrl + Enter</small>
          </div>
          <ErrorNotice error={command.error} />
        </form>
      </div>
    </section>
  );
}
