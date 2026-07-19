// Shared terminal font-size preference (A− / A+ controls): one localStorage
// key used by both the TermOverlay panel and the dedicated /term/:sessionId
// page, so the chosen size follows the user across both surfaces.

import { useEffect, useState } from 'react'
import { DEFAULT_TERM_FONT_SIZE } from '../components/TermPanel'

export const FONT_SIZE_STORAGE_KEY = 'rocket.term.fontSize'
export const FONT_SIZE_STEP = 1
export const FONT_SIZE_MIN = 12
export const FONT_SIZE_MAX = 20

export function loadStoredFontSize(): number {
  if (typeof window === 'undefined') return DEFAULT_TERM_FONT_SIZE
  const raw = window.localStorage.getItem(FONT_SIZE_STORAGE_KEY)
  const parsed = raw ? Number(raw) : NaN
  if (!Number.isFinite(parsed)) return DEFAULT_TERM_FONT_SIZE
  return Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, parsed))
}

export interface TermFontSize {
  fontSize: number
  shrink: () => void
  grow: () => void
  canShrink: boolean
  canGrow: boolean
}

/** Persisted terminal font size with clamped A−/A+ steppers. */
export function useTermFontSize(): TermFontSize {
  const [fontSize, setFontSize] = useState<number>(loadStoredFontSize)

  useEffect(() => {
    window.localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(fontSize))
  }, [fontSize])

  return {
    fontSize,
    shrink: () => setFontSize((size) => Math.max(FONT_SIZE_MIN, size - FONT_SIZE_STEP)),
    grow: () => setFontSize((size) => Math.min(FONT_SIZE_MAX, size + FONT_SIZE_STEP)),
    canShrink: fontSize > FONT_SIZE_MIN,
    canGrow: fontSize < FONT_SIZE_MAX,
  }
}
