// Settings › Prices (task #5138 spec §5): $ per 1M tokens for every model the
// usage stats have met, so the Usage screens can show ≈ $. The list starts
// empty-priced on purpose — no built-in prices that go stale. Cost is
// computed at read time, so a saved price moves every usage view at once.
// Only the human edits prices; the daemon answers 403 human_only to agents.

import { useState } from 'react'
import { Button } from '../../components/Button'
import { useDeletePrice, usePrices, useSetPrice } from '../../lib/queries'
import type { ModelPrice, ModelPriceInput } from '../../lib/types'

const KINDS: Array<{ key: keyof ModelPriceInput; label: string }> = [
  { key: 'input', label: 'input' },
  { key: 'cache_write', label: 'cache write' },
  { key: 'cache_read', label: 'cache read' },
  { key: 'output', label: 'output' },
]

type Draft = Record<keyof ModelPriceInput, string>

function draftOf(p: ModelPrice): Draft {
  const text = (v: number | null) => (v === null ? '' : String(v))
  return { input: text(p.input), cache_write: text(p.cache_write), cache_read: text(p.cache_read), output: text(p.output) }
}

/** Empty → null (not set); anything else must be a number ≥ 0. Undefined on a bad field. */
function parseDraft(d: Draft): ModelPriceInput | undefined {
  const out = {} as ModelPriceInput
  for (const { key } of KINDS) {
    const raw = d[key].trim()
    if (raw === '') {
      out[key] = null
      continue
    }
    const n = Number(raw)
    if (!Number.isFinite(n) || n < 0) return undefined
    out[key] = n
  }
  return out
}

function PriceRow({ price }: { price: ModelPrice }) {
  const setPrice = useSetPrice()
  const deletePrice = useDeletePrice()
  const initial = draftOf(price)
  const [draft, setDraft] = useState<Draft>(initial)
  const [invalid, setInvalid] = useState(false)
  const dirty = KINDS.some(({ key }) => draft[key].trim() !== initial[key])
  const priced = KINDS.some(({ key }) => price[key] !== null)
  const busy = setPrice.isPending || deletePrice.isPending

  function save() {
    const body = parseDraft(draft)
    setInvalid(body === undefined)
    if (!body) return
    deletePrice.reset()
    setPrice.mutate({ model: price.model, ...body })
  }

  function clear() {
    setInvalid(false)
    setPrice.reset()
    deletePrice.mutate(price.model)
  }

  const error = invalid ? 'A price is a number ≥ 0, or empty for “not set”.' : (setPrice.error ?? deletePrice.error)?.message

  return (
    <tr>
      <td className="settings-prices__model">
        <div className="settings-mono">{price.model}</div>
        {error && (
          <p className="settings-error" role="alert">
            {error}
          </p>
        )}
      </td>
      {KINDS.map(({ key, label }) => (
        <td key={key}>
          <input
            className="settings-prices__input"
            inputMode="decimal"
            aria-label={`${price.model} ${label}`}
            placeholder="—"
            value={draft[key]}
            onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
          />
        </td>
      ))}
      <td className="settings-prices__actions">
        <Button variant="primary" size="sm" onClick={save} disabled={!dirty || busy}>
          {setPrice.isPending ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" size="sm" onClick={clear} disabled={!priced || busy}>
          Clear
        </Button>
      </td>
    </tr>
  )
}

// A row starts over from the server whenever its saved prices change.
const rowKey = (p: ModelPrice) => [p.model, ...KINDS.map(({ key }) => p[key])].join('|')

export function PricesSection() {
  const prices = usePrices()
  const list = prices.data ?? []

  return (
    <section>
      <h1 className="settings-section__title">Prices</h1>
      <p className="settings-section__subtitle">
        $ per 1M tokens, used for the ≈ $ on the Usage screens. A model without a price shows “—” and stays out of the
        sum.
      </p>
      <div className="settings-card">
        {prices.isError && <p className="settings-error">Could not load prices: {prices.error.message}</p>}
        {prices.isSuccess && list.length === 0 && (
          <p className="settings-field__hint">No models yet — they appear here once agent usage is counted.</p>
        )}
        {list.length > 0 && (
          <table className="settings-models__table settings-prices__table">
            <thead>
              <tr>
                <th>Model</th>
                <th>Input</th>
                <th>Cache write</th>
                <th>Cache read</th>
                <th>Output</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.map((p) => (
                <PriceRow key={rowKey(p)} price={p} />
              ))}
            </tbody>
          </table>
        )}
        <p className="settings-field__hint">Reasoning tokens are part of output and priced as output.</p>
      </div>
    </section>
  )
}
