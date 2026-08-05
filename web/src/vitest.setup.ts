import '@testing-library/jest-dom/vitest'

// Node 26 ships its own `localStorage` global, disabled unless the process is
// started with --localstorage-file. Being a global, it shadows the working one
// jsdom installs on `window`, so every test that touches storage dies on
// "Cannot read properties of undefined (reading 'getItem')" — nothing to do
// with the code under test. Give the suite a plain in-memory implementation.
if (typeof window !== 'undefined') {
  let usable = false
  try {
    // Not `?.` — an absent localStorage is exactly the case to replace, and
    // optional chaining would quietly report it as working.
    usable = typeof window.localStorage.getItem('probe') !== 'undefined' || true
  } catch {
    usable = false
  }
  if (!usable) {
    const store = new Map<string, string>()
    const memoryStorage: Storage = {
      get length() {
        return store.size
      },
      key: (i: number) => [...store.keys()][i] ?? null,
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, String(v)),
      removeItem: (k: string) => void store.delete(k),
      clear: () => store.clear(),
    }
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: memoryStorage,
    })
  }
}
