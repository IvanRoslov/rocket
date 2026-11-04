import { api, actionPath, useCommand } from './api'
import type { Scope, ShownVersion, Work } from './types'
import { actionsFor } from './workflow'
import { ErrorNotice } from './ui'
export function ReviewActions({scope,work,canProgress,shown,ready,onShowReview}: {scope:Scope;work:Work;canProgress:boolean;shown:ShownVersion[];ready:boolean;onShowReview:()=>void}) {
 const command=useCommand()
 const resultIDs=shown.filter(s=>s.artifact.kind==='result').map(s=>s.version.id)
 const review=work.artifact_version_ids ?? []
 const shownIDs=shown.map(s=>s.version.id)
 const bound=ready && review.every(id=>shownIDs.includes(id)) && resultIDs.every(id=>review.includes(id))
 const reviewing=work.status==='awaiting_approval'
 const labels:Record<string,string>={start:'Start work',submit:'Submit shown results',approve:'Approve shown results',reject:'Request changes',reopen:'Reopen for rework'}
 return <section className="review-actions">{reviewing && <><h2>Review submitted results</h2><p>Approval applies to this submitted set:</p>{review.length===0?<p>No artifacts were submitted with this review.</p>:<ul>{review.map(id=>{const entry=shown.find(s=>s.version.id===id);return <li key={id}>{entry?`${entry.artifact.name} · version ${entry.version.version_number}`:'Submitted artifact — load its version to inspect'}</li>})}</ul>}{!bound && <><p>Return to the submitted versions before approving or requesting changes.</p><button onClick={onShowReview}>Show submitted versions</button></>}</>}{work.status==='completed' && <p>Reopen this {scope.item_id?'item':'stage'} to revise it. Later stages and their results are preserved. This work will need approval again according to its saved process.</p>}<div className="row">{actionsFor(work.status,canProgress).map(action=><button key={action} className={action==='approve'?'primary':''} disabled={command.isPending || (['approve','reject'].includes(action) && !bound) || (action==='submit' && !ready)} onClick={()=>{
 const body=action==='submit'?{action,artifact_version_ids:resultIDs}:['approve','reject'].includes(action)?{action,review_revision:work.review_revision}:{action}
 command.mutate(()=>api.post(actionPath(scope),body))
 }}>{labels[action]}</button>)}</div><ErrorNotice error={command.error}/></section>
}
