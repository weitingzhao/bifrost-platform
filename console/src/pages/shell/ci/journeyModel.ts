import type { TimingRun } from '@/api/ciTimings'
import { elapsed, reusedBy, runCommit, runStatus, seconds } from './timingModel'

export function commitJourney(commit: string, ci: TimingRun[], merges: TimingRun[], deliveries: TimingRun[], selectedRun?: string) {
  const same = (runs: TimingRun[]) => runs.filter(r => runCommit(r) === commit).sort((a,b) => Date.parse(a.start_time ?? a.created_at) - Date.parse(b.start_time ?? b.created_at))
  const attempts = same(ci), lands = same(merges), ships = same(deliveries)
  const first = selectedRun ? attempts.find(r => r.name === selectedRun) : attempts[0]
  const afterStart = (r: TimingRun) => !selectedRun || (!!first?.start_time && Date.parse(r.completion_time ?? "") >= Date.parse(first.start_time))
  const landed = lands.find(r => runStatus(r) === 'Succeeded' && afterStart(r))
  const delivered = ships.find(r => runStatus(r) === 'Succeeded' && afterStart(r))
  const tested = attempts.filter(r => !reusedBy(r))
  return {
    attempts, lands, ships, tested, first, landed, delivered,
    failed: attempts.filter(r => runStatus(r) === 'Failed').length,
    reused: attempts.filter(r => !!reusedBy(r)).length,
    total: first && delivered ? seconds(first.start_time, delivered.completion_time) : null,
    toMain: first && landed ? seconds(first.start_time, landed.completion_time) : null,
    afterMain: landed && delivered ? seconds(landed.completion_time, delivered.start_time) : null,
    delivery: delivered ? elapsed(delivered) : null,
  }
}
