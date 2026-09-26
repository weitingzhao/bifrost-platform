import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const SELF = fileURLToPath(import.meta.url)

/**
 * The 1a material (@bifrost/ui 0.5.0, design Rev .59–.74; Owner ruling
 * 2026-09-25: Ops adopts it, no framed legacy look kept). Groups are
 * `--card-fill` with a transparent `--card-border`, rules inside a group are
 * `--table-rule`, tags are a currentColor capsule, controls and fields are
 * `--control-fill` / `--field-fill`. None of them is the neutral 1px
 * `--border` frame, so a new one of those is a regression — the page still
 * renders, it just reads as the old console again.
 *
 * What stays neutral on purpose: dashed and dotted frames (an empty slot, not
 * a group), and the marks listed below.
 */
const NEUTRAL = String.raw`(?:border|input|sidebar-border|\[var\(--(?:border|input|sidebar-border)\)\])`
const NEUTRAL_CLASS = new RegExp(
  String.raw`(?:^|[\s'"\x60])((?:[\w-]+:)*(?:border|divide)(?:-[trblxy])?-${NEUTRAL}(?:\/\d+)?)(?=[\s'"\x60]|$)`,
  'g',
)

/** Neutral frame classes still allowed, per file, with the count that is there today. */
const KNOWN_NEUTRAL: ReadonlyMap<string, { count: number; why: string }> = new Map([
  ['components/delivery/PluginStepCommandCenter.tsx', { count: 1, why: 'pending step ring (24px) — the hollow ring is the reading' }],
  ['components/delivery/ReleaseStepCommandCenter.tsx', { count: 1, why: 'pending step ring (24px) — the hollow ring is the reading' }],
  ['components/task-mode/TaskPhaseProgress.tsx', { count: 2, why: 'planned / unknown step rings (24px)' }],
  ['components/task-mode/DailyOpsProcessStrip.tsx', { count: 1, why: 'planned step ring (24px)' }],
  ['components/task-mode/operator-plan/ChecklistSection.tsx', { count: 1, why: '16px checkbox box' }],
])

/** Rule blocks that may still draw a neutral border: marks, not frames. */
const KNOWN_NEUTRAL_CSS: ReadonlyMap<string, string> = new Map([
  ['.remediation-step-dot', 'pending step dot (0.75rem) — the ring is the reading'],
])

function walk(dir: string, test: (name: string) => boolean, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    if (statSync(full).isDirectory()) walk(full, test, out)
    else if (test(entry)) out.push(full)
  }
  return out
}

/** Neutral frame classes per file, skipping string literals that draw a dashed or dotted frame. */
function neutralClassCounts(): Map<string, string[]> {
  const found = new Map<string, string[]>()
  const sources = walk(SRC, (name) => /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name))
  for (const file of sources) {
    if (file === SELF) continue
    const body = readFileSync(file, 'utf8')
    for (const lit of body.matchAll(/'[^'\n]*'|"[^"\n]*"|`[^`]*`/g)) {
      if (/\bborder-(?:dashed|dotted)\b/.test(lit[0])) continue
      for (const m of lit[0].matchAll(NEUTRAL_CLASS)) {
        const rel = path.relative(SRC, file)
        found.set(rel, [...(found.get(rel) ?? []), m[1]])
      }
    }
  }
  return found
}

/**
 * Border / box-shadow declarations that draw `--border` itself or a fade of it
 * (`color-mix(in srgb, var(--border) 80%, transparent)`). A state colour mixed
 * toward `--border` — `color-mix(in srgb, var(--color-lamp-red) 45%, var(--border))`
 * — is a reading and passes, as does any rule block that draws a dashed or
 * dotted frame. Backgrounds are not checked: resize grips and stepper dots
 * paint with `--border` and are not frames.
 */
function neutralCssDeclarations(): { selector: string; where: string }[] {
  const out: { selector: string; where: string }[] = []
  for (const file of walk(SRC, (name) => name.endsWith('.css'))) {
    const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
    for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const body = block[2]
      if (/\b(?:dashed|dotted)\b/.test(body)) continue
      for (const decl of body.split(';')) {
        const colon = decl.indexOf(':')
        if (colon < 0) continue
        const prop = decl.slice(0, colon).trim()
        if (!/^(?:border|outline|box-shadow)/.test(prop)) continue
        // The first colour may carry its own fallback: var(--task-mode-accent, #f59e0b).
        const value = decl
          .slice(colon + 1)
          .replace(
            /color-mix\(in \w+,\s*(?!var\(--(?:border|input|sidebar-border)\))(?:var\([^()]*\)|[^,()\s]+)\s*\d*%?,\s*var\(--border\)\)/g,
            '',
          )
        if (/var\(--(?:border|input|sidebar-border)\)/.test(value)) {
          const selector = block[1].trim().split('\n').pop() ?? ''
          out.push({ selector, where: `${path.relative(SRC, file)} — ${selector} { ${prop} }` })
        }
      }
    }
  }
  return out
}

describe('1a material: no neutral 1px --border frames in Ops-owned markup', () => {
  it('has no neutral frame classes beyond the known marks', () => {
    const over = [...neutralClassCounts()]
      .filter(([file, classes]) => classes.length > (KNOWN_NEUTRAL.get(file)?.count ?? 0))
      .map(([file, classes]) => `${file}: ${classes.join(' ')}`)
    expect(over).toEqual([])
  })

  it('keeps the known list shrinking, never growing', () => {
    // An allowance nobody uses any more must come off the list, or the next
    // frame added to that file slips through under it.
    const found = neutralClassCounts()
    const stale = [...KNOWN_NEUTRAL]
      .filter(([file, { count }]) => (found.get(file)?.length ?? 0) < count)
      .map(([file, { count }]) => `${file}: allowed ${count}, found ${found.get(file)?.length ?? 0}`)
    expect(stale).toEqual([])
  })

  it('draws no neutral frame or rule from the stylesheets', () => {
    const found = neutralCssDeclarations()
      .filter(({ selector }) => !KNOWN_NEUTRAL_CSS.has(selector))
      .map(({ where }) => where)
    expect(found).toEqual([])
  })

  it('keeps the known stylesheet marks in use', () => {
    const selectors = new Set(neutralCssDeclarations().map(({ selector }) => selector))
    expect([...KNOWN_NEUTRAL_CSS.keys()].filter((sel) => !selectors.has(sel))).toEqual([])
  })
})
