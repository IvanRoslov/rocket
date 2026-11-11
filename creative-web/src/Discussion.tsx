import { Button } from "../../web/src/components/Button";
import { useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, base, useCommand } from "./api";
import { FeedReadMarker } from "./FeedReadMarker";
import { FeedEntryView } from "./FeedEntry";
import { ChatContext } from "./ChatContext";
import { ChatHistory, scrollToEntry } from "./ChatHistory";
import { QuestionArchive } from "./QuestionArchive";
import { cacheFeedEntry, useFeed } from "./feed";
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
  const [body, setBody] = useState("");
  const [draftContext, setDraftContext] = useState<Context>();
  const command = useCommand();
  const client = useQueryClient();
  const target = draftContext ?? context;
  const changed = JSON.stringify(target) !== JSON.stringify(context);
  function reset() {
    setBody("");
    setDraftContext(undefined);
    command.reset();
  }
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
  useLayoutEffect(() => {
    const window = windowRef.current;
    if (!window || !active) return;
    function fit() {
      const top = window!.getBoundingClientRect().top;
      const height = globalThis.innerWidth <= 600 ? globalThis.innerHeight - 90 : globalThis.innerHeight - Math.max(60, top) - 48;
      window!.style.setProperty("--chat-height", `${Math.max(300, height)}px`);
    }
    fit();
    globalThis.addEventListener("resize", fit);
    return () => globalThis.removeEventListener("resize", fit);
  }, [selectionKey, active, ready]);
  const stageLabel = stageName ?? context.label.split(" / ")[1] ?? context.label;
  const itemLabel = items.find(item => item.id === context.item_id)?.name;
  const targetLabel = target.label.split(" · ")[0];
  return (
    <section className="discussion">
      <div className="chat-window" ref={windowRef}>
        <header className="chat-heading">
          <h2>{context.stage_id ? `Stage chat «${stageLabel}»` : "Run chat"}</h2>
          {context.item_id && <span>{itemLabel ?? `Item ${context.item_id}`}</span>}
          {context.item_id && <label className="feed-filter"><input type="checkbox" checked={onlyItem} onChange={(e) => setFilteredSelection(e.target.checked ? selectionKey : undefined)} />Only this item</label>}
        </header>
        {pending.length > 0 && <button className="chat-pending" onClick={() => {
          const history = windowRef.current?.querySelector<HTMLElement>(".chat-history");
          if (history) scrollToEntry(history, pending[pendingIndex.current++ % pending.length].id);
        }}>Awaiting decision: {pending.length} <span>View proposal ↓</span></button>}
        <ErrorNotice error={feed.error} />
        <FeedReadMarker key={`read:${selectionKey}:${onlyItem}`} scope={feedScope} active={active}
          loaded={feed.data !== undefined} through={sorted.at(-1)?.id ?? 0} updatedAt={feed.dataUpdatedAt} />
        <ChatHistory key={`history:${selectionKey}:${onlyItem}`} entries={sorted} scope={feedScope} active={active}>
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
        <form className="composer question-thread__form" onSubmit={(e) => {
          e.preventDefault();
          if (!body.trim() || command.isPending || (!ready && !draftContext)) return;
          const { run_id, stage_id, item_id, artifact_version_ids } = target;
          command.mutate(async () => {
            const entry = await api.post<FeedEntry>(`${base}/runs/${run_id}/feed`, {
              ...(stage_id ? { stage_id } : {}), ...(item_id ? { item_id } : {}), artifact_version_ids, body,
            });
            await cacheFeedEntry(client, entry);
          }, { onSuccess: reset });
        }}>
          <ChatContext label={targetLabel} ids={target.artifact_version_ids} changed={changed} />
          <label className="chat-input">Message<textarea rows={2} value={body} onChange={(e) => {
            if (!draftContext) setDraftContext({ ...context, artifact_version_ids: [...context.artifact_version_ids] });
            setBody(e.target.value);
          }} onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
              e.preventDefault(); e.currentTarget.form?.requestSubmit();
            }
          }} placeholder="Write to the orchestrator…" disabled={command.isPending || (!ready && !draftContext)} /></label>
          <div className="row">
            <Button variant="primary" size="sm" type="submit" disabled={!body.trim() || command.isPending || (!ready && !draftContext)}>Send message</Button>
            {body && <Button variant="secondary" size="sm" type="button" onClick={reset} disabled={command.isPending}>Discard draft</Button>}
            <small>⌘ / Ctrl + Enter</small>
          </div>
          <ErrorNotice error={command.error} />
        </form>
      </div>
      <QuestionArchive context={context} />
    </section>
  );
}
