import { expect, it } from 'vitest'
import { commitJourney } from '../journeyModel'
import { timingRun } from './timingFixture'

// Origin: 2026-11-15 relay had a failed CI/retry and a same-tree amended commit.
// Owner needs honest end-to-end time, not a sum of overlapping runs or reuse time.
it('includes failed attempts and waits without adding overlapping merge duration', () => {
 const base=timingRun(), commit=base.revision!
 const retry={...base,name:'retry',start_time:'2026-11-15T20:02:00Z',completion_time:'2026-11-15T20:03:00Z'}
 const merge={...base,name:'land',completion_time:'2026-11-15T20:03:10Z'}
 const ship={...base,name:'ship',start_time:'2026-11-15T20:04:00Z',completion_time:'2026-11-15T20:07:00Z'}
 const reused={...base,name:'reuse',results:[{name:'tested-by',value:'retry'}]}
 const out=commitJourney(commit,[{...base,status:'Failed'},retry,reused],[merge],[ship])
 expect(out.total).toBe(420);expect(out.toMain).toBe(190);expect(out.afterMain).toBe(50);expect(out.delivery).toBe(180)
 expect(out.failed).toBe(1);expect(out.reused).toBe(1);expect(out.tested).toHaveLength(2)
})
// Same finding: a queued/refused delivery or another commit is not a zero-second success.
it('does not invent completion from running or other-commit deliveries', () => {
 const base=timingRun()
 expect(commitJourney(base.revision!,[base],[],[{...base,status:'Running',completion_time:undefined}]).total).toBeNull()
 expect(commitJourney(base.revision!,[base],[],[{...base,revision:'b'.repeat(40)}]).total).toBeNull()
 expect(commitJourney(base.revision!,[],[],[base]).total).toBeNull()
})

// A later selected attempt must not be timed from an earlier CI or earlier delivery.
it('anchors the journey to the selected attempt and excludes earlier completed releases', () => {
 const base=timingRun(), commit=base.revision!
 const retry={...base,name:'retry',start_time:'2026-11-15T20:05:00Z',completion_time:'2026-11-15T20:06:00Z'}
 const earlier={...base,name:'old-ship',completion_time:'2026-11-15T20:04:00Z'}
 const later={...base,name:'new-ship',start_time:'2026-11-15T20:07:00Z',completion_time:'2026-11-15T20:10:00Z'}
 const out=commitJourney(commit,[base,retry],[base],[earlier,later],'retry')
 expect(out.first?.name).toBe('retry');expect(out.total).toBe(300);expect(out.delivered?.name).toBe('new-ship');expect(out.toMain).toBeNull()
 expect(commitJourney(commit,[base],[],[earlier],'missing').total).toBeNull()
})
