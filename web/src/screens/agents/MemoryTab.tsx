// Role file memory (docs/10-agents.md): MEMORY.md — the index that is pasted
// into every briefing — plus the fact files beside it. Both are editable here;
// the daemon only accepts plain `.md` base names inside the role's memory dir.

import { useState } from 'react'
import { Button } from '../../components/Button'
import { Markdown } from '../../components/Markdown'
import { timeAgo } from '../../lib/format'
import { useAgentMemory, useUpdateAgentMemory } from '../../lib/queries'
import './agents.css'

const INDEX_NAME = 'MEMORY.md'

interface MemoryFileCardProps {
  roleId: string
  name: string
  body: string
  updatedAt?: number
  label: string
}

function MemoryFileCard({ roleId, name, body, updatedAt, label }: MemoryFileCardProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(body)
  const update = useUpdateAgentMemory()

  function startEditing() {
    setDraft(body)
    setEditing(true)
  }

  function save() {
    update.mutate({ id: roleId, file: name, body: draft }, { onSuccess: () => setEditing(false) })
  }

  return (
    <div className="agent-memory__file">
      <div className="agent-memory__file-header">
        <span className="agent-memory__file-name">{name}</span>
        {updatedAt !== undefined && (
          <span className="agent-memory__file-meta">updated {timeAgo(updatedAt)}</span>
        )}
        <div className="agent-memory__spacer" />
        {!editing && (
          <Button variant="secondary" size="sm" onClick={startEditing}>
            Edit
          </Button>
        )}
      </div>
      <div className="agent-memory__body">
        {editing ? (
          <>
            <textarea
              className="agent-memory__editor"
              aria-label={label}
              rows={16}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            {update.isError && <p className="agent-form__error">{update.error.message}</p>}
            <div className="agent-memory__editor-actions">
              <Button variant="primary" size="sm" onClick={save} disabled={update.isPending}>
                Save
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setEditing(false)}>
                Cancel
              </Button>
            </div>
          </>
        ) : (
          <Markdown compact>{body || '_empty_'}</Markdown>
        )}
      </div>
    </div>
  )
}

export interface MemoryTabProps {
  roleId: string
}

export function MemoryTab({ roleId }: MemoryTabProps) {
  const { data: memory } = useAgentMemory(roleId)
  const [newName, setNewName] = useState('')

  if (!memory) return null

  return (
    <div className="agent-memory">
      <div className="agent-memory__path">{memory.path}</div>

      <MemoryFileCard
        roleId={roleId}
        name={INDEX_NAME}
        body={memory.index}
        label="Memory index"
        // The index is inlined into every briefing, so its own mtime is not
        // interesting next to the facts it points at.
      />

      {memory.files.map((file) => (
        <MemoryFileCard
          key={file.name}
          roleId={roleId}
          name={file.name}
          body={file.body}
          updatedAt={file.updated_at}
          label={`Edit ${file.name}`}
        />
      ))}

      <div className="agent-tab__toolbar">
        <label htmlFor="agent-memory-new">New fact file</label>
        <input
          id="agent-memory-new"
          className="agent-tab__select"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="platform.md"
        />
        <NewFactButton roleId={roleId} name={newName} onCreated={() => setNewName('')} />
      </div>
    </div>
  )
}

function NewFactButton({
  roleId,
  name,
  onCreated,
}: {
  roleId: string
  name: string
  onCreated: () => void
}) {
  const update = useUpdateAgentMemory()

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        disabled={!name.trim() || update.isPending}
        onClick={() =>
          update.mutate(
            { id: roleId, file: name.trim(), body: '' },
            { onSuccess: onCreated },
          )
        }
      >
        Create
      </Button>
      {update.isError && <span className="agent-form__error">{update.error.message}</span>}
    </>
  )
}
