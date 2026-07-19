import { describe, expect, it } from 'vitest'
import { worstCiState } from './TaskCard'

describe('worstCiState', () => {
  it('returns undefined for no states', () => {
    expect(worstCiState([])).toBeUndefined()
  })

  it('returns the single state when only one is present', () => {
    expect(worstCiState(['passing'])).toBe('passing')
  })

  it('prefers failing over passing', () => {
    expect(worstCiState(['passing', 'failing'])).toBe('failing')
  })

  it('prefers pending over passing', () => {
    expect(worstCiState(['passing', 'pending'])).toBe('pending')
  })

  it('returns failing when workers have both failing and pending', () => {
    expect(worstCiState(['pending', 'failing'])).toBe('failing')
    expect(worstCiState(['failing', 'pending'])).toBe('failing')
  })
})
