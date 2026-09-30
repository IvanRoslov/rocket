import { classifyUserEntry } from './chatDisplay'

describe('classifyUserEntry', () => {
  it('subagent hand-back is a system row labeled agent message', () => {
    const text = 'Another Claude session sent a message:\n<agent-message from="w1">done</agent-message>'
    expect(classifyUserEntry(text)).toEqual({ kind: 'system', label: 'agent message', body: text })
  })

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

  it('rocket injects are system rows labeled by their tag', () => {
    expect(classifyUserEntry('[rocket heartbeat] worker idle 6m')).toEqual({
      kind: 'system',
      label: 'rocket heartbeat',
      body: 'worker idle 6m',
    })
    expect(classifyUserEntry('[rocket] delivery FAILED to w1').kind).toBe('system')
  })

  it('SYSTEM NOTIFICATION markers are system wherever they appear', () => {
    expect(classifyUserEntry('pre [SYSTEM NOTIFICATION - NOT USER INPUT] body').kind).toBe('system')
  })

  it('thread frames become visible thread rows', () => {
    expect(classifyUserEntry('[#4543/Q2 answer from human] go')).toEqual({
      kind: 'thread',
      ref: '#4543/Q2',
      label: 'answer from human',
      body: 'go',
    })
    expect(classifyUserEntry('[cto/Q1 reply from w1] **done**\nPR #3')).toEqual({
      kind: 'thread',
      ref: 'cto/Q1',
      label: 'reply from w1',
      body: '**done**\nPR #3',
    })
  })

  it('a human message starting with an unrelated bracket stays human', () => {
    expect(classifyUserEntry('[draft] my notes')).toEqual({ kind: 'human' })
  })
})
