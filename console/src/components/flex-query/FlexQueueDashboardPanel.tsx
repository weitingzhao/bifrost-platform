import { DenseTag } from '@bifrost/ui'
import type { FlexQueueSummary } from '@/api/flexQueryPlugin'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

export function FlexQueueDashboardPanel({
  counts,
  loading,
  error,
}: {
  counts: FlexQueueSummary | null | undefined
  loading: boolean
  error: string | null
}) {
  const pending = counts?.pending ?? 0
  const running = counts?.running ?? 0
  const done = counts?.done ?? 0
  const failed = counts?.failed ?? 0
  const idle = pending + running === 0
  const active = pending + running
  const cap = Math.max(active, 1)

  return (
    <OpsSection
      title="Queue dashboard"
      description="flex_ops.job_flex_ingest counts"
      headerExtra={
        <DenseTag variant={failed > 0 ? 'danger' : idle ? 'success' : 'info'}>
          {idle ? 'idle' : 'active'}
        </DenseTag>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading queue…
        </p>
      ) : error != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : (
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={idle ? 1 : running}
            thin={idle ? 0 : pending}
            blocked={failed}
            total={Math.max(idle ? 1 : running + pending + failed, 1)}
            caption={idle ? 'idle' : 'run'}
          />
          <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-4">
            <DashCard
              title="Pending"
              value={fmtCount(pending)}
              rawValue={pending}
              invertFlash={pending > 0}
              unit="jobs"
            >
              <Meter
                fillPct={(pending / cap) * 100}
                toneClass={toneByLevel(pending > 0 ? 'scheduled' : 'ok')}
              />
            </DashCard>
            <DashCard
              title="Running"
              value={fmtCount(running)}
              rawValue={running}
              unit="jobs"
            >
              <Meter
                fillPct={(running / cap) * 100}
                toneClass={toneByLevel(running > 0 ? 'ok' : 'unknown')}
              />
            </DashCard>
            <DashCard
              title="Done"
              value={fmtCount(done)}
              rawValue={done}
              unit="jobs"
            >
              <Meter
                fillPct={Math.min(100, done > 0 ? 100 : 0)}
                toneClass={toneByLevel(done > 0 ? 'ok' : 'unknown')}
              />
            </DashCard>
            <DashCard
              title="Failed"
              value={fmtCount(failed)}
              rawValue={failed}
              invertFlash={failed > 0}
              unit="jobs"
            >
              <Meter
                fillPct={failed > 0 ? 100 : 0}
                toneClass={toneByLevel(failed > 0 ? 'missing' : 'ok')}
              />
            </DashCard>
          </div>
        </div>
      )}
    </OpsSection>
  )
}
