/** How often an open page compares itself with the console the server would serve now. */
export const CONSOLE_BUILD_CHECK_MS = 3 * 60_000

export const NEWER_CONSOLE_BANNER = 'A newer Console has been deployed. Reload before approving.'

export const DECISION_AFTER_RELOAD =
  'A newer Console has been deployed. Wait for the in-flight decision to finish and reload before making another decision.'

/** The module script a fresh index.html names, or null when the page does not say. */
export function moduleScriptSrc(html: string): string | null {
  const re = /<script\b([^>]*)>/gi
  for (const match of html.matchAll(re)) {
    const attrs = match[1] ?? ''
    if (!/\btype\s*=\s*(['"])module\1/i.test(attrs)) continue
    const src = /\bsrc\s*=\s*(['"])([^'"]+)\1/i.exec(attrs)?.[2]
    if (src != null && src !== '') return src
  }
  return null
}

/** The module script this document was started with. */
export function ownModuleScriptSrc(doc: Document = document): string | null {
  for (const node of doc.querySelectorAll('script[type="module"][src]')) {
    const src = node.getAttribute('src')
    if (src != null && src !== '') return src
  }
  return null
}

/** Identity is the script path. A host or query on the same file is still the same build. */
export function buildIdentity(src: string): string {
  try {
    return new URL(src, 'http://console.local').pathname
  } catch {
    return src
  }
}

export function buildsDiffer(own: string, server: string): boolean {
  return buildIdentity(own) !== buildIdentity(server)
}
