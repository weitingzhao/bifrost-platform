import { useEffect, useState } from 'react'

export type Appearance = 'light' | 'dark' | 'system'
export type VisualStyle = 'gradient' | 'grayscale'
export const APPEARANCE_KEY = 'bifrost-ops-appearance'
export const STYLE_KEY = 'bifrost-ops-visual-style'

function stored(key: string): string | null {
  try { return window.localStorage.getItem(key) } catch { return null }
}

export function readAppearance(): Appearance {
  const value = stored(APPEARANCE_KEY)
  return value === 'light' || value === 'dark' ? value : 'system'
}

export function readVisualStyle(): VisualStyle {
  return stored(STYLE_KEY) === 'grayscale' ? 'grayscale' : 'gradient'
}

function save(key: string, value: string) {
  try { window.localStorage.setItem(key, value) } catch { /* A private session can still change its appearance. */ }
}

export function applyAppearance(appearance: Appearance, style: VisualStyle): boolean {
  const dark = appearance === 'dark' || (appearance === 'system' && window.matchMedia?.('(prefers-color-scheme: dark)').matches === true)
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.dataset.opsTheme = dark ? 'dark' : 'light'
  document.documentElement.dataset.opsStyle = style
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', style === 'grayscale' ? (dark ? '#242d36' : '#e9edf0') : (dark ? '#232b40' : '#e1e8f6'))
  return dark
}

/** Preferences are independent of routes, query caches, auth and unsent drafts. */
export function useConsoleAppearance() {
  const [appearance, setAppearance] = useState(readAppearance)
  const [style, setStyle] = useState(readVisualStyle)
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  useEffect(() => {
    const update = () => setDark(applyAppearance(appearance, style))
    const media = window.matchMedia?.('(prefers-color-scheme: dark)')
    update()
    media?.addEventListener('change', update)
    return () => media?.removeEventListener('change', update)
  }, [appearance, style])
  useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === null || event.key === APPEARANCE_KEY) setAppearance(readAppearance())
      if (event.key === null || event.key === STYLE_KEY) setStyle(readVisualStyle())
    }
    window.addEventListener('storage', sync)
    return () => window.removeEventListener('storage', sync)
  }, [])
  return {
    appearance, style, dark,
    chooseAppearance(value: Appearance) { save(APPEARANCE_KEY, value); setAppearance(value) },
    chooseStyle(value: VisualStyle) { save(STYLE_KEY, value); setStyle(value) },
  }
}
