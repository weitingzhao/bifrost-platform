import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../..')
const AGENT_FILES = [
  'agent/remediation/src/prompt.ts',
  'agent/remediation/src/tools/deliveryTools.ts',
]

/**
 * TD-230 gave the release gate a third result, 'inconclusive'. TD-249 required
 * the remediation agent to name that result. Phase 3 removed the release-gate
 * tool and its copy. Any line that still describes a gate result as pass/fail
 * must also name inconclusive.
 */
describe('remediation agent gate copy names inconclusive (TD-249)', () => {
  for (const rel of AGENT_FILES) {
    it(rel, () => {
      const lines = readFileSync(path.join(REPO, rel), 'utf8').split('\n')
      const offenders = lines
        .map((line, i) => ({ line, n: i + 1 }))
        .filter(({ line }) => /gate/i.test(line) && /pass\s*\/\s*fail/i.test(line) && !/inconclusive/.test(line))
        .map(({ line, n }) => `${rel}:${n} ${line.trim()}`)
      expect(offenders).toEqual([])
    })
  }
})
