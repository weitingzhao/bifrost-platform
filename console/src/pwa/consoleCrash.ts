import type { ErrorInfo } from 'react'
import { ownModuleScriptSrc } from '@/pwa/consoleBuild'

/*
 * The whole Console had no error boundary: one render error, or one lazy chunk a
 * newer deploy had already removed, unmounted the tree and left a white page with
 * nothing to read (2026-11-17 STG: one unknown roster status did it; 2026-11-23 the
 * Owner saw a white preview whose cause nobody could capture). This screen keeps
 * the evidence on the page itself: the error, which build the page runs, where it
 * was and the viewport, so a screenshot is enough to diagnose it.
 */

const CHUNK_LOAD_ERROR =
  /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|ChunkLoadError|Loading (CSS )?chunk .* failed/i

/** A lazy chunk could not be fetched: the page's code is older than what the server holds. */
export function isChunkLoadError(error: unknown): boolean {
  if (!(error instanceof Error)) return false
  return error.name === 'ChunkLoadError' || CHUNK_LOAD_ERROR.test(error.message)
}

export interface CrashReport {
  message: string
  chunk: boolean
  build: string | null
  at: string
  viewport: string
  when: string
  component: string | null
}

export function crashReport(error: unknown, info: ErrorInfo | null, win: Window = window, now: Date = new Date()): CrashReport {
  const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error)
  const component = info?.componentStack?.trim().split('\n')[0]?.trim() ?? null
  return {
    message,
    chunk: isChunkLoadError(error),
    build: ownModuleScriptSrc(win.document),
    at: win.location.hash || '/',
    viewport: `${win.innerWidth}×${win.innerHeight} @${win.devicePixelRatio}x`,
    when: now.toISOString(),
    component: component || null,
  }
}

export function crashReportText(r: CrashReport): string {
  return [
    `error: ${r.message}`,
    `chunk load failure: ${r.chunk ? 'yes' : 'no'}`,
    `build: ${r.build ?? 'unknown'}`,
    `at: ${r.at}`,
    `viewport: ${r.viewport}`,
    `when: ${r.when}`,
    `first component: ${r.component ?? 'unknown'}`,
  ].join('\n')
}
