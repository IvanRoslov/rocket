import {
  addresseeLabel,
  addresseePayload,
  answerableBy,
  countYourTurn,
  isHuman,
  participantInitial,
  participantLabel,
  toggleAddressee,
} from './threads'

describe('isHuman', () => {
  // The wire sends "" today and "human" after subtask #736. Both are us.
  it('recognises the human in both wire forms', () => {
    expect(isHuman('')).toBe(true)
    expect(isHuman('human')).toBe(true)
    expect(isHuman(undefined)).toBe(true)
  })

  it('does not mistake an agent or a session for the human', () => {
    expect(isHuman('cto')).toBe(false)
    expect(isHuman('reply-answer-orch')).toBe(false)
  })
})

describe('participantLabel', () => {
  it('labels both human forms as "you"', () => {
    expect(participantLabel('')).toBe('you')
    expect(participantLabel('human')).toBe('you')
  })

  it('labels anyone else by their id', () => {
    expect(participantLabel('cto')).toBe('cto')
  })
})

describe('participantInitial', () => {
  it('gives the human a Y and everyone else their first letter', () => {
    expect(participantInitial('human')).toBe('Y')
    expect(participantInitial('')).toBe('Y')
    expect(participantInitial('cto')).toBe('C')
  })
})

describe('addresseeLabel', () => {
  it('is empty when nobody is addressed', () => {
    expect(addresseeLabel([])).toBe('')
    expect(addresseeLabel(undefined)).toBe('')
  })

  it('names the addressees, the human included', () => {
    expect(addresseeLabel(['cto'])).toBe('→ cto')
    expect(addresseeLabel(['human', 'cto'])).toBe('→ you, cto')
  })
})

describe('answerableBy', () => {
  it('offers every participant except the human', () => {
    expect(answerableBy(['human', 'reply-answer-orch', 'cto'])).toEqual(['reply-answer-orch', 'cto'])
  })

  it('tolerates the legacy empty human id', () => {
    expect(answerableBy(['', 'cto'])).toEqual(['cto'])
  })
})

describe('toggleAddressee', () => {
  it('adds then removes', () => {
    expect(toggleAddressee([], 'cto')).toEqual(['cto'])
    expect(toggleAddressee(['cto'], 'cto')).toEqual([])
  })

  it('keeps several addressees', () => {
    expect(toggleAddressee(['cto'], 'orch')).toEqual(['cto', 'orch'])
  })
})

describe('addresseePayload', () => {
  // "None picked" must send no `to` key at all — the daemon then falls back to
  // "everyone except the author", which is a different thing from `to: []`.
  it('omits the key when nobody is picked', () => {
    expect(addresseePayload([])).toEqual({})
    expect('to' in addresseePayload([])).toBe(false)
  })

  it('sends the picked addressees', () => {
    expect(addresseePayload(['cto'])).toEqual({ to: ['cto'] })
  })
})

describe('countYourTurn', () => {
  it('counts only open threads waiting on us', () => {
    const threads = [
      { status: 'open', your_turn: true },
      { status: 'open', your_turn: false },
      { status: 'resolved', your_turn: true },
      { status: 'open' },
    ]
    expect(countYourTurn(threads)).toBe(1)
  })

  it('is 0 for no threads', () => {
    expect(countYourTurn([])).toBe(0)
  })
})
