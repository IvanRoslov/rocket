// The inbox's storm rows (task #4901, spec v2 §3.1): one per task whose storm
// has questions waiting on you. A storm question is answered only in the
// task's Brainstorm tab, so the row is a link there, never a card here.

import { Link } from 'react-router-dom'
import { stormHref, stormLabel, type StormGroup } from '../../lib/storm'

export function StormRows({
  storms,
  className,
  currentTaskId,
}: {
  storms: StormGroup[]
  className: string
  /** The row the Decide cursor is on, if any. */
  currentTaskId?: number
}) {
  return (
    <>
      {storms.map((g) => (
        <Link
          key={g.taskId}
          to={stormHref(g)}
          aria-current={g.taskId === currentTaskId ? 'true' : undefined}
          className={g.taskId === currentTaskId ? `${className} q__qrow--on` : className}
        >
          <span className="q__qrow-head">
            <span className="q__qrow-ref">Storm</span>
            {g.stale && <span className="q__stale-tag">STALE</span>}
          </span>
          <span className="q__qrow-body">{stormLabel(g)}</span>
          <span className="q__opt-hint">answer in the Brainstorm tab →</span>
        </Link>
      ))}
    </>
  )
}

/**
 * A storm row picked in Decide (J/K): the pane says where it is answered and
 * how to get there — Enter or any number key opens the Brainstorm tab.
 */
export function StormCard({ storm }: { storm: StormGroup }) {
  return (
    <div className="q__card">
      <div className="q__card-head">
        <span className="q__ref">Storm</span>
        <span className="q__subject">#{storm.taskId} {storm.taskTitle}</span>
      </div>
      <h2 className="q__question">{stormLabel(storm)}</h2>
      <div className="q__body">
        Storm questions are answered in the task’s Brainstorm tab, next to the problem and the spec they
        shape.
      </div>
      <div className="q__act-row">
        <Link className="q__answer" to={stormHref(storm)}>
          Open the Brainstorm tab<span className="q__answer-key">↩</span>
        </Link>
      </div>
    </div>
  )
}
