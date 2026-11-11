import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "../../web/src/components/Button";
import { Markdown } from "../../web/src/components/Markdown";
import { api } from "./api";
import { ChatContext } from "./ChatContext";
import { AttachmentCards } from "./AttachmentCards";
import { cacheFeedEntry, cardPath, CardConflict, decideCard, refreshFeeds } from "./feed";
import type { FeedDecision, FeedEntry, Item } from "./types";
import { ErrorNotice } from "./ui";

const decisions: Record<FeedDecision, string> = { go: "Go", revise: "Revise", deny: "Deny" };
function EntryTime({ value }: { value: number }) {
  return <time dateTime={new Date(value * 1000).toISOString()}>{new Date(value * 1000).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })}</time>;
}
export function FeedEntryView({ entry, items, onJump, contextLabel = "Run", readOnly = false }: {
  readOnly?: boolean;
  entry: FeedEntry;
  contextLabel?: string;
  items: Pick<Item, "id" | "name">[];
  onJump: (successor: FeedEntry) => void;
}) {
  const client = useQueryClient();
  const decision = useMutation({
    mutationFn: async (value: FeedDecision) => {
      try {
        const updated = await decideCard(entry.run_id, entry.id, value);
        await cacheFeedEntry(client, updated);
      } catch (error) {
        if (error instanceof CardConflict) await cacheFeedEntry(client, error.entry);
        throw error;
      }
    },
    onSuccess: () => refreshFeeds(client, entry.run_id),
    onError: (error) => {
      if (error instanceof CardConflict) void refreshFeeds(client, entry.run_id);
    },
  });
  const successor = useMutation({
    mutationFn: () => api.get<FeedEntry>(cardPath(entry.run_id, entry.card!.replaced_by_entry_id)),
    onSuccess: onJump,
  });
  const card = entry.card;
  return (
    <article id={`feed-entry-${entry.id}`} tabIndex={-1}
      className={`feed-entry feed-entry--${entry.author}${card ? ` feed-entry--${card.status}` : ""}`}
      aria-label={`${card ? "Proposal" : "Message"} ${entry.id}`}>
      <header className="feed-entry__meta">
        <strong>{entry.author === "human" ? "You" : "Orchestrator"}</strong>
        <EntryTime value={entry.created_at} />
        {!!entry.item_id && <span className="feed-item-tag">{items.find(item => item.id === entry.item_id)?.name ?? `Item ${entry.item_id}`}</span>}
        {card?.status === "pending" && <span className="feed-entry__pending-label">Awaiting your decision</span>}
      </header>
      <Markdown compact>{entry.body}</Markdown>
      {entry.author === "human" ? <ChatContext label={items.find(item => item.id === entry.item_id)?.name ?? contextLabel} ids={entry.artifact_version_ids} /> : <AttachmentCards ids={entry.artifact_version_ids} items={items} />}
      {!readOnly && card?.status === "pending" && <div className="row feed-entry__actions">
        {(Object.keys(decisions) as FeedDecision[]).map(value => <Button key={value} variant={value === "go" ? "primary" : "secondary"} size="sm"
          disabled={decision.isPending} onClick={() => decision.mutate(value)}>{decisions[value]}</Button>)}
      </div>}
      {card?.status === "decided" && <p className="feed-entry__decision"><strong>Decision: {card.decision && decisions[card.decision]}</strong> · <EntryTime value={card.decided_at} /></p>}
      {card?.status === "replaced" && <p className="feed-entry__replacement"><a href={`#feed-entry-${card.replaced_by_entry_id}`} aria-disabled={successor.isPending}
        onClick={event => { event.preventDefault(); if (!successor.isPending) successor.mutate(); }}>Replaced → proposal #{card.replaced_by_entry_id}</a></p>}
      <ErrorNotice error={decision.error ?? successor.error} />
    </article>
  );
}
