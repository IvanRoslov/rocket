/* Shared presentation only; routing stays with each application. */
export function RocketBrand() {
  return (
    <>
      <div
        style={{
          width: 22,
          height: 22,
          borderRadius: 6,
          background: 'var(--text)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--surface)',
          font: '700 12px var(--font-mono)',
        }}
      >
        R
      </div>
      <span style={{ font: '600 14px var(--font-mono)', color: 'var(--text)', letterSpacing: '-.02em' }}>
        rocket
      </span>
    </>
  )
}
