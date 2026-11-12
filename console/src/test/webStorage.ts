// CI runs these tests on Node 20 (.nvmrc). From Node 22 on, Node has its own
// localStorage and sessionStorage globals; without --localstorage-file they
// are objects with no getItem. Vitest's jsdom environment does not replace a
// global that already exists, so on a newer local Node every test that goes
// through authedFetch failed with "window.localStorage.getItem is not a
// function" while CI stayed green (5 tests on Node 25.2, 2026-11-12).
// Hand the two names back to jsdom's storages when that has happened.
const dom = (globalThis as { jsdom?: { window: Record<string, unknown> } }).jsdom

function usable(name: string): boolean {
  try {
    return typeof (globalThis as unknown as Record<string, Storage | undefined>)[name]?.getItem === 'function'
  } catch {
    return false
  }
}

if (dom != null) {
  for (const name of ['localStorage', 'sessionStorage']) {
    if (!usable(name)) {
      Object.defineProperty(globalThis, name, { configurable: true, get: () => dom.window[name] })
    }
  }
}

export {}
