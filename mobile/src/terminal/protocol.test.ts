import { buildWsUrl, SPECIAL_KEYS } from './protocol'

describe('buildWsUrl', () => {
  it('converts http to ws and appends the term path', () => {
    expect(buildWsUrl('http://192.168.1.10:4477', 'abc')).toBe('ws://192.168.1.10:4477/v1/sessions/abc/term')
  })
  it('adds readonly flag', () => {
    expect(buildWsUrl('http://h:1', 's', true)).toBe('ws://h:1/v1/sessions/s/term?readonly=true')
  })
  it('escapes the session id', () => {
    expect(buildWsUrl('http://h:1', 'a/b')).toBe('ws://h:1/v1/sessions/a%2Fb/term')
  })
})

describe('SPECIAL_KEYS', () => {
  const seq = (label: string) => SPECIAL_KEYS.find((k) => k.label === label)?.seq
  it('covers control keys with correct sequences', () => {
    expect(seq('esc')).toBe('\x1b')
    expect(seq('tab')).toBe('\t')
    expect(seq('^C')).toBe('\x03')
    expect(seq('^D')).toBe('\x04')
    expect(seq('⏎')).toBe('\r')
  })
  it('uses CSI sequences for arrows', () => {
    expect(seq('↑')).toBe('\x1b[A')
    expect(seq('↓')).toBe('\x1b[B')
    expect(seq('←')).toBe('\x1b[D')
    expect(seq('→')).toBe('\x1b[C')
  })
})
