export const SERVICE_WORKER_URL = '/sw.js'

/**
 * Registers the home-screen app's service worker (offline shell; web push mounts
 * here later). Production builds in a secure context only — browsers refuse it on
 * plain http, and the dev server must never be cached.
 */
export function registerServiceWorker(
  env: { prod: boolean } = { prod: import.meta.env.PROD },
  win: Window = window,
): boolean {
  if (!env.prod || !win.isSecureContext || !('serviceWorker' in win.navigator)) return false
  win.addEventListener('load', () => {
    void win.navigator.serviceWorker.register(SERVICE_WORKER_URL, { scope: '/' }).catch(() => undefined)
  })
  return true
}
