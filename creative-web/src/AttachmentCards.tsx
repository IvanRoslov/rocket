import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, base } from "./api";
import { Media } from "./Artifacts";
import type { ArtifactDetail, ArtifactVersion, Item } from "./types";

type Items = Pick<Item, "id" | "name">[];
export function AttachmentCards({ ids, items }: { ids: number[]; items: Items }) {
  const [expanded, setExpanded] = useState(false);
  if (!ids.length) return null;
  return <div className="chat-attachments" aria-label="Attachments">
    {(expanded ? ids : ids.slice(0, 3)).map(id => <AttachmentCard key={id} id={id} items={items} />)}
    {ids.length > 3 && <button className="chat-attachments__more" onClick={() => setExpanded(!expanded)}>{expanded ? "Show fewer" : `+${ids.length - 3} more`}</button>}
  </div>;
}
function AttachmentCard({ id, items }: { id: number; items: Items }) {
  const [open, setOpen] = useState(false);
  const [failed, setFailed] = useState(false);
  const version = useQuery({ queryKey: ["creative", "version", id], queryFn: () => api.get<ArtifactVersion>(`${base}/artifact-versions/${id}`) });
  const detail = useQuery({ queryKey: ["creative", "artifact", version.data?.artifact_id], enabled: !!version.data, queryFn: () => api.get<ArtifactDetail>(`${base}/artifacts/${version.data!.artifact_id}`) });
  const v = version.data, artifact = detail.data?.artifact;
  if (version.error || detail.error) return <p className="muted">Referenced version is unavailable</p>;
  if (!v) return <p className="muted">Loading referenced version…</p>;
  const mime = (v.mime ?? "").toLowerCase();
  const html = /^text\/html(;|$)/.test(mime);
  const item = items.find(item => item.id === artifact?.item_id);
  return <div className="chat-attachment">
    <a className="chat-attachment__card" href={html ? `${base}/artifact-versions/${id}/view` : v.content_url}
      target={html ? "_blank" : undefined} rel={html ? "noopener noreferrer" : undefined}
      aria-expanded={html ? undefined : open}
      onClick={html ? undefined : event => { event.preventDefault(); setOpen(!open); }}>
      {!failed && mime.startsWith("image/") ? <img src={v.content_url} alt={v.filename} loading="lazy" onError={() => setFailed(true)} /> :
        !failed && mime.startsWith("video/") ? <video src={v.content_url} preload="metadata" muted playsInline onError={() => setFailed(true)} /> :
          <span className="chat-attachment__icon" aria-hidden="true">{mime.startsWith("audio/") ? "♫" : html ? "〈/〉" : "▤"}</span>}
      <span className="chat-attachment__info"><strong>{artifact?.name ?? v.filename}</strong><span>{item?.name ?? (artifact?.item_id ? `Item ${artifact.item_id}` : "Stage / run")} · version {v.version_number}</span></span>
    </a>
    {open && <Media version={v} />}
  </div>;
}
