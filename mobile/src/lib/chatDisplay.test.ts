import { classifyUserEntry } from './chatDisplay'

describe('classifyUserEntry', () => {
  it('plain human text stays human', () => {
    expect(classifyUserEntry('давай без миграции, просто фикс кода')).toEqual({ kind: 'human' })
  })

  it('markdown from a human stays human', () => {
    expect(classifyUserEntry('## план\n- пункт')).toEqual({ kind: 'human' })
  })

  it('inter-agent mail becomes an agent bubble with sender', () => {
    const d = classifyUserEntry('[from billing-v2-ui] blocked on the API contract')
    expect(d).toEqual({ kind: 'agent', from: 'billing-v2-ui', body: 'blocked on the API contract' })
  })

  it('task-notification wrapper collapses into a system row', () => {
    const d = classifyUserEntry('<task-notification>\n<result>…</result>\n</task-notification>')
    expect(d.kind).toBe('system')
    expect((d as { label: string }).label).toBe('task-notification')
  })

  it('system-reminder collapses into a system row', () => {
    expect(classifyUserEntry('<system-reminder>\nstuff\n</system-reminder>').kind).toBe('system')
  })

  it('question funnel deliveries are labeled Q&A', () => {
    const d = classifyUserEntry('[task #12 QM answer] roll it forward')
    expect(d).toEqual({ kind: 'system', label: 'Q&A · task #12 answer', body: 'roll it forward' })
  })

  it('heartbeat and large-message pointers are system', () => {
    expect(classifyUserEntry('[heartbeat] worker idle 6m').kind).toBe('system')
    expect(classifyUserEntry('[large message] Full text written to …').kind).toBe('system')
  })

  it('a human message merely mentioning a < later is not system', () => {
    expect(classifyUserEntry('use a < b in the check')).toEqual({ kind: 'human' })
  })
})
