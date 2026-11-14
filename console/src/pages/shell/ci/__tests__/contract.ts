import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { FullCiNotes, UnitsConfig, UnitsRuns } from '@/api/units'

/*
 * The bodies the units routes answer, as the back end keeps them: its own tests compare these files with what the
 * routes compute. The Console's CI tests read them here and nowhere copy them.
 */
const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../../api/internal/server/testdata/contract/units')
const read = <T,>(name: string): T => JSON.parse(fs.readFileSync(path.join(dir, name), 'utf8')) as T

export const contract = {
  configDeclared: () => read<UnitsConfig>('config-declared.json'),
  configUndeclared: () => read<UnitsConfig>('config-undeclared.json'),
  runs: () => read<UnitsRuns>('runs.json'),
  runsNone: () => read<UnitsRuns>('runs-none.json'),
  fullCi: () => read<FullCiNotes>('full-ci.json'),
  fullCiNone: () => read<FullCiNotes>('full-ci-none.json'),
}
