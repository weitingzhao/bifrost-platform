type BadgingNavigator = Navigator & {
  setAppBadge?: (contents?: number) => Promise<void>
  clearAppBadge?: () => Promise<void>
}

/**
 * Home-screen icon count = Needs you count. `null` (unknown) clears the icon rather
 * than showing zero. Browsers without the Badging API (or without a secure context)
 * ignore it.
 */
export function syncAppBadge(count: number | null, nav: Navigator = navigator): void {
  const badging = nav as BadgingNavigator
  if (typeof badging.setAppBadge !== 'function' || typeof badging.clearAppBadge !== 'function') return
  const done = count != null && count > 0 ? badging.setAppBadge(count) : badging.clearAppBadge()
  void done.catch(() => undefined)
}
