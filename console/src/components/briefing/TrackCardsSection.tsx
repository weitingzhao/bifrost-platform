import type { TrackSummary, TrackId } from '@/lib/briefing/workTracks'
import { BriefingIconBadge, TRACK_ICONS } from '@/lib/briefing/briefingIcons'
import { StatusLamp } from '@/components/StatusLamp'

interface TrackCardsSectionProps {
  tracks: TrackSummary[]
  selectedTrack: TrackId | null
  onSelectTrack: (id: TrackId) => void
}

function trackReach(t: TrackSummary): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (t.id === 'operate') {
    if (t.issues.length === 0) return 'ok'
    if (t.issues.some(i => i.kind === 'cluster_failing' || i.kind === 'matrix_fail')) return 'fail'
    return 'degraded'
  }
  if (t.progress == null) return 'unknown'
  if (t.progress.percent === 100) return 'ok'
  if (t.progress.percent > 0) return 'degraded'
  return 'unknown'
}

function ProgressBar({ done, total, percent }: { done: number; total: number; percent: number }) {
  return (
    <div className="mt-2">
      <div className="flex items-center justify-between text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <span>{done}/{total}</span>
        <span>{percent}%</span>
      </div>
      <div className="mt-0.5 h-1.5 w-full overflow-hidden rounded-full bg-[var(--border)]">
        <div
          className="h-full rounded-full bg-[var(--primary)] transition-all"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  )
}

export function TrackCardsSection({ tracks, selectedTrack, onSelectTrack }: TrackCardsSectionProps) {
  return (
    <section className="page-section panel-elevated px-4 py-3">
      <p className="briefing-section-kicker m-0">1 · Work tracks</p>
      <h2 className="m-0 mt-1 text-sm font-semibold">Where is the project right now?</h2>
      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Three persistent tracks with progress and next steps. Select one to scope your <strong>Session briefing</strong>.
      </p>

      <div className="mt-3 grid gap-3 sm:grid-cols-3">
        {tracks.map(t => {
          const selected = selectedTrack === t.id
          const reach = trackReach(t)

          return (
            <button
              key={t.id}
              type="button"
              className={[
                'flex flex-col rounded-lg border px-3 py-2.5 text-left transition-colors',
                selected
                  ? 'border-[var(--primary)] bg-[var(--secondary)]'
                  : 'border-[var(--border)] bg-[var(--card)] hover:bg-[var(--secondary)]',
              ].join(' ')}
              onClick={() => onSelectTrack(t.id)}
            >
              <div className="flex items-start gap-2.5">
                <BriefingIconBadge icon={TRACK_ICONS[t.id]} selected={selected} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <StatusLamp value={reach} kind="reach" />
                    <span className="text-sm font-semibold capitalize">{t.id}</span>
                    {t.currentPhase && (
                      <span className="ml-auto rounded bg-[var(--border)] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
                        {t.currentPhase}
                      </span>
                    )}
                  </div>

                  <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    {t.subtitle}
                  </p>

                  {t.progress != null && (
                    <ProgressBar done={t.progress.done} total={t.progress.total} percent={t.progress.percent} />
                  )}

                  {t.id === 'operate' && t.issues.length > 0 && (
                    <ul className="m-0 mt-2 flex list-none flex-col gap-0.5 p-0 text-[var(--text-dense-meta)]">
                      {t.issues.slice(0, 3).map((issue, i) => (
                        <li key={i} className="flex items-start gap-1">
                          <StatusLamp
                            value={
                              issue.kind === 'matrix_fail' || issue.kind === 'cluster_failing'
                                ? 'fail'
                                : 'degraded'
                            }
                            kind="reach"
                          />
                          <span className="text-[var(--muted-foreground)]">{issue.label}</span>
                        </li>
                      ))}
                    </ul>
                  )}

                  {t.nextStep && (
                    <p className="m-0 mt-2 text-[var(--text-dense-meta)]">
                      <span className="text-[var(--muted-foreground)]">Next: </span>
                      <span className="text-[var(--foreground)]">{t.nextStep}</span>
                    </p>
                  )}
                </div>
              </div>
            </button>
          )
        })}
      </div>
    </section>
  )
}
