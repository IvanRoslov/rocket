import { Button } from "../../web/src/components/Button";
import { Markdown } from "../../web/src/components/Markdown";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, base, useCommand } from "./api";
import { BoundVersions } from "./BoundVersions";
import { QuestionArchive } from "./QuestionArchive";
import { cacheFeedEntry, useFeed } from "./feed";
import type { Context, FeedEntry, Item } from "./types";
import { Empty, ErrorNotice } from "./ui";

export function Discussion({ context, ready, items = [] }: {
  context: Context;
  orchestrator: string;
  ready: boolean;
  items?: Pick<Item, "id" | "name">[];
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
  const sorted = [...(feed.data ?? [])].sort((a, b) => b.id - a.id);
  return (
    <section className="discussion">
      <div className="heading"><div><span className="eyebrow">Creative orchestrator</span><h2>Conversation</h2></div></div>
      <p className="context-label">{context.label}</p>
      <form className="composer question-thread__form" onSubmit={(e) => {
        e.preventDefault();
        const { run_id, stage_id, item_id, artifact_version_ids } = target;
        command.mutate(async () => {
          const entry = await api.post<FeedEntry>(`${base}/runs/${run_id}/feed`, {
          ...(stage_id ? { stage_id } : {}), ...(item_id ? { item_id } : {}), artifact_version_ids, body,
          });
          await cacheFeedEntry(client, entry);
        }, { onSuccess: reset });
      }}>
        <p className="draft-context">{changed ? `Draft stays with ${target.label}` : `About ${target.label}`}</p>
        <label>Message<textarea rows={4} value={body} onChange={(e) => {
          if (!draftContext) setDraftContext({ ...context, artifact_version_ids: [...context.artifact_version_ids] });
          setBody(e.target.value);
        }} placeholder="Write to the orchestrator about the selected work and versions." disabled={command.isPending || (!ready && !draftContext)} /></label>
        <div className="row">
          <Button variant="primary" size="sm" type="submit" disabled={!body.trim() || command.isPending}>Send message</Button>
          {body && <Button variant="secondary" size="sm" type="button" onClick={reset} disabled={command.isPending}>Discard draft</Button>}
        </div>
        <ErrorNotice error={command.error} />
      </form>
      <div className="discussion-feed">
        <h3>Newest first</h3>
        {context.item_id && <label className="feed-filter"><input type="checkbox" checked={onlyItem} onChange={(e) => setFilteredSelection(e.target.checked ? selectionKey : undefined)} />Only this item</label>}
        <ErrorNotice error={feed.error} />
        {feed.isPending && <p>Loading conversation…</p>}
        {feed.data?.length === 0 && <Empty>No messages in this feed.</Empty>}
        <ol className="conversation-entries" aria-label="Conversation entries">
          {sorted.map(entry => <li key={entry.id}><article id={`feed-entry-${entry.id}`} aria-label={`Message ${entry.id}`}>
            <header className="feed-entry__meta"><strong>{entry.author === "human" ? "You" : "Orchestrator"}</strong><time dateTime={new Date(entry.created_at * 1000).toISOString()}>{new Date(entry.created_at * 1000).toLocaleString()}</time>
              {!!entry.item_id && <span className="feed-item-tag">{items.find(item => item.id === entry.item_id)?.name ?? `Item ${entry.item_id}`}</span>}
            </header>
            <Markdown compact>{entry.body}</Markdown>
            <BoundVersions ids={entry.artifact_version_ids} />
          </article></li>)}
        </ol>
      </div>
      <QuestionArchive context={context} />
    </section>
  );
}
