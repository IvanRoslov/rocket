import { ago, bytes, sessionBadge, sessionDot, stripAnsi, uptime } from './format'
import { colors } from '../theme'

describe('ago', () => {
  const now = 1_750_000_000
  beforeEach(() => jest.spyOn(Date, 'now').mockReturnValue(now * 1000))
  afterEach(() => jest.restoreAllMocks())

  it('handles missing ts', () => expect(ago(undefined)).toBe(''))
  it('just now under a minute', () => expect(ago(now - 30)).toBe('just now'))
  it('minutes', () => expect(ago(now - 12 * 60)).toBe('12m ago'))
  it('hours', () => expect(ago(now - 3 * 3600 - 10)).toBe('3h ago'))
  it('days', () => expect(ago(now - 2 * 86400)).toBe('2d ago'))
})

describe('bytes', () => {
  it('bytes', () => expect(bytes(512)).toBe('512 B'))
  it('kilobytes', () => expect(bytes(4 * 1024)).toBe('4 KB'))
  it('megabytes', () => expect(bytes(240 * 1024 * 1024)).toBe('240 MB'))
  it('gigabytes', () => expect(bytes(1.8 * 1024 * 1024 * 1024)).toBe('1.8 GB'))
})

describe('uptime', () => {
  it('minutes only', () => expect(uptime(300)).toBe('5m'))
  it('hours and minutes', () => expect(uptime(3 * 3600 + 120)).toBe('3h 2m'))
  it('days and hours', () => expect(uptime(2 * 86400 + 4 * 3600)).toBe('2d 4h'))
})

describe('stripAnsi', () => {
  it('strips SGR color codes', () => {
    expect(stripAnsi('\x1b[31mred\x1b[0m plain')).toBe('red plain')
  })
  it('strips cursor movement', () => {
    expect(stripAnsi('a\x1b[2Ab\x1b[10;20Hc')).toBe('abc')
  })
  it('strips OSC title sequences', () => {
    expect(stripAnsi('\x1b]0;my title\x07hello')).toBe('hello')
  })
  it('keeps plain text untouched', () => {
    expect(stripAnsi('plain $ ls -la')).toBe('plain $ ls -la')
  })
})

describe('sessionDot', () => {
  it('errored is red', () => expect(sessionDot('errored')).toBe(colors.red))
  it('blocked activity is red', () => expect(sessionDot('running', 'blocked')).toBe(colors.red))
  it('active is green', () => expect(sessionDot('running', 'active')).toBe(colors.green))
  it('idle is amber', () => expect(sessionDot('running', 'idle')).toBe(colors.amber))
  it('done is slate', () => expect(sessionDot('done')).toBe(colors.slate))
})

describe('sessionBadge', () => {
  it('running+activity uses activity label', () => {
    expect(sessionBadge('running', 'ready')).toEqual({ label: 'ready', fg: colors.greenFg, bg: colors.greenBg })
  })
  it('killed uses state label', () => {
    expect(sessionBadge('killed').label).toBe('killed')
  })
  it('blocked is red', () => {
    expect(sessionBadge('running', 'blocked').fg).toBe(colors.redFg)
  })
})
