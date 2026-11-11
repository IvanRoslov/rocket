import { useState } from "react";
import { BoundVersions } from "./BoundVersions";

// Keep the automatically bound version set out of the reading flow.
export function ChatContext({ label, ids, changed = false }: { label: string; ids: number[]; changed?: boolean }) {
  const [open, setOpen] = useState(false);
  if (!ids.length) return null;
  return <details className="chat-context" onToggle={event => setOpen(event.currentTarget.open)}>
    <summary>{changed ? `Draft stays with ${label}` : `About: ${label}`} · {ids.length} versions</summary>
    {open && <BoundVersions ids={ids} />}
  </details>;
}
