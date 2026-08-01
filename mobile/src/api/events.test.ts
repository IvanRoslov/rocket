import { keyMatches, parseEventType } from './events'

describe('parseEventType', () => {
  it('session events touch sessions, system, task and projects', () => {
    expect(parseEventType('session.state_changed')).toEqual(['sessions', 'system', 'task', 'projects'])
    expect(parseEventType('session.spawned')).toContain('sessions')
  })
  it('task events touch tasks and task detail', () => {
    expect(parseEventType('task.question_asked')).toEqual(['tasks', 'task', 'projects'])
  })
  it('message events touch messages and system', () => {
    expect(parseEventType('message.delivered')).toEqual(['messages', 'system'])
  })
  it('pr events touch tasks and sessions', () => {
    expect(parseEventType('pr.merged')).toEqual(['tasks', 'task', 'sessions'])
  })
  it('chat pings do not invalidate anything', () => {
    expect(parseEventType('session.chat_updated')).toEqual([])
  })
  it('agent events touch the roles list and role detail', () => {
    expect(parseEventType('agent.question_asked')).toEqual(['agents', 'agent'])
    expect(parseEventType('agent.instance_spawned')).toEqual(['agents', 'agent'])
  })
  it('unknown events map to nothing', () => {
    expect(parseEventType('weird.thing')).toEqual([])
    expect(parseEventType('')).toEqual([])
  })
})

describe('keyMatches', () => {
  const BASE = 'http://10.0.0.5:4477'
  it('matches by second key segment', () => {
    expect(keyMatches([BASE, 'sessions', 'all'], ['sessions'])).toBe(true)
    expect(keyMatches([BASE, 'task', 12], ['tasks', 'task'])).toBe(true)
  })
  it('rejects other segments', () => {
    expect(keyMatches([BASE, 'settings'], ['sessions'])).toBe(false)
  })
  it('rejects malformed keys', () => {
    expect(keyMatches([BASE], ['sessions'])).toBe(false)
    expect(keyMatches([BASE, 42], ['sessions'])).toBe(false)
  })
})
