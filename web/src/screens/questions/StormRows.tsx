// The inbox's storm rows (task #4901, spec v2 §3.1): one per task whose storm
// has questions waiting on you. A storm question is answered only in the
// task's Brainstorm tab, so the row is a link there, never a card here.

import { Link } from 'react-router-dom'
import { stormHref, stormLabel, type StormGroup } from '../../lib/storm'

export function StormRows({ storms, className }: { storms: StormGroup[]; className: string }) {
  return (
    <>
      {storms.map((g) => (
        <Link key={g.taskId} to={stormHref(g)} className={className}>
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
