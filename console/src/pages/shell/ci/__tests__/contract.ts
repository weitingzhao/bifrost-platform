import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { ApprovalReadings } from '@/api/readings'
import type { FullCiNotes, UnitsConfig, UnitsRuns } from '@/api/units'

/*
 * The bodies the units and readings routes answer, as the back end keeps them: its own tests compare these files with what the
 * routes compute. The Console's CI tests read them here and nowhere copy them.
 */
const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../../api/internal/server/testdata/contract/units')
const readingsDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../../api/internal/server/testdata/contract/readings')
const read = <T,>(name: string, from: string = dir): T => JSON.parse(fs.readFileSync(path.join(from, name), 'utf8')) as T

export const contract = {
  configDomains: () => read<UnitsConfig>('config-domains.json'),
  configDeclared: () => read<UnitsConfig>('config-declared.json'),
  configUndeclared: () => read<UnitsConfig>('config-undeclared.json'),
  runs: () => read<UnitsRuns>('runs.json'),
  runsNone: () => read<UnitsRuns>('runs-none.json'),
  fullCi: () => read<FullCiNotes>('full-ci.json'),
  fullCiNone: () => read<FullCiNotes>('full-ci-none.json'),
  approvals: () => read<ApprovalReadings>('approvals.json', readingsDir),
  approvalsNone: () => read<ApprovalReadings>('approvals-none.json', readingsDir),
}
