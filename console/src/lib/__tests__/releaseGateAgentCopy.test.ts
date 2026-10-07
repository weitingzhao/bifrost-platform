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
 * TD-230 gave the release gate a third result, 'inconclusive' (a required check
 * was not measured). TD-249: the remediation agent's own copy still said
 * pass/fail, so an agent could read an unmeasured gate as passed. Any line in
 * the agent copy that describes a gate result as pass/fail must also name
 * inconclusive.
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
      expect(lines.join('\n')).toMatch(/inconclusive/)
    })
  }
})
