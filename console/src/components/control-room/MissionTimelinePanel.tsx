import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { StatusLamp } from '@bifrost/ui'
import { Bot, ChevronDown, ChevronRight, Clock, History, RotateCcw } from 'lucide-react'
import { fetchAgentNightlyReport, fetchAudit, fetchRemediationJobs } from '@/api/platform'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import {
  buildMissionTimelineModel,
  missionTimelineToneToLamp,
  type MissionTimelineEvent,
} from '@/lib/control-room/missionTimeline'
import { formatRemediationJobWhen } from '@/lib/remediation/remediationJobDisplay'

interface MissionTimelinePanelProps {
  snapshot: MissionSnapshot
  probeObservedAt?: number
  onOpenAudit?: () => void
  onOpenAgentDesk?: (jobId: string) => void
}

function TimelineEventRow({
  event,
  onOpenAudit,
  onOpenAgentDesk,
}: {
  event: MissionTimelineEvent
  onOpenAudit?: () => void
  onOpenAgentDesk?: (jobId: string) => void
}) {
  const lamp = missionTimelineToneToLamp(event.tone)

  return (
    <li className="mission-timeline__event">
      <span className="mission-timeline__lamp">
        <StatusLamp value={lamp} kind="reach" />
      </span>
      <div className="mission-timeline__event-body">
        <div className="mission-timeline__event-head">
          <span className="mission-timeline__event-title">{event.title}</span>
          <time className="mission-timeline__event-time" dateTime={event.at}>
            {formatRemediationJobWhen(event.at)}
          </time>
        </div>
        <p className="mission-timeline__event-detail">{event.detail}</p>
        <div className="mission-timeline__event-actions">
          {event.jobId != null && onOpenAgentDesk != null && (
            <button
              type="button"
              className="mission-timeline__action"
              onClick={() => onOpenAgentDesk(event.jobId!)}
            >
              <Bot size={12} />
              Agent Desk
            </button>
          )}
          {event.kind === 'audit_actuation' && onOpenAudit != null && (
            <button type="button" className="mission-timeline__action" onClick={onOpenAudit}>
              <History size={12} />
              Audit
            </button>
          )}
        </div>
      </div>
    </li>
  )
}

export function MissionTimelinePanel({
  snapshot,
  probeObservedAt,
  onOpenAudit,
  onOpenAgentDesk,
}: MissionTimelinePanelProps) {
  const [expanded, setExpanded] = useState(false)

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 15_000,
  })
  const auditQuery = useQuery({
    queryKey: ['platform', 'audit'],
    queryFn: fetchAudit,
    refetchInterval: 30_000,
  })
  const nightlyQuery = useQuery({
    queryKey: ['agent', 'nightly-report'],
    queryFn: fetchAgentNightlyReport,
    refetchInterval: 60_000,
  })

  const model = useMemo(
    () =>
      buildMissionTimelineModel({
        jobs: jobsQuery.data?.jobs ?? [],
        auditRecords: auditQuery.data?.records ?? [],
        nightlyReport: nightlyQuery.data,
        snapshot,
        probeObservedAt,
      }),
    [jobsQuery.data, auditQuery.data, nightlyQuery.data, snapshot, probeObservedAt],
  )

  const loading = jobsQuery.isLoading || auditQuery.isLoading

  return (
    <section className="mission-timeline" aria-label="Mission timeline">
      <button
        type="button"
        className="mission-timeline__toggle"
        onClick={() => setExpanded(v => !v)}
        aria-expanded={expanded}
      >
        {expanded ? (
          <ChevronDown className="mission-timeline__chevron" size={14} />
        ) : (
          <ChevronRight className="mission-timeline__chevron" size={14} />
        )}
        <Clock size={14} className="mission-timeline__icon" />
        <span className="mission-timeline__title">Mission timeline</span>
        <span className="mission-timeline__window">{model.windowLabel}</span>
        {!loading && model.events.length > 0 && (
          <span className="mission-timeline__count">{model.events.length} events</span>
        )}
        {onOpenAudit != null && (
          <span
            role="button"
            tabIndex={0}
            className="mission-timeline__audit-link"
            onClick={e => {
              e.stopPropagation()
              onOpenAudit()
            }}
            onKeyDown={e => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                onOpenAudit()
              }
            }}
          >
            <History size={12} />
            Open Audit
          </span>
        )}
      </button>

      {expanded && (
        <div className="mission-timeline__body">
          {model.nightlySummary != null && (
            <p className="mission-timeline__nightly">{model.nightlySummary}</p>
          )}

          {model.trajectories.length > 0 && (
            <div className="mission-timeline__trajectories">
              <span className="mission-timeline__trajectories-label">Trajectories</span>
              <div className="mission-timeline__trajectory-list">
                {model.trajectories.slice(0, 4).map(traj => (
                  <button
                    key={traj.jobId}
                    type="button"
                    className="mission-timeline__trajectory"
                    onClick={() => onOpenAgentDesk?.(traj.jobId)}
                    title={`${traj.label} · ${traj.status} · started ${formatRemediationJobWhen(traj.startedAt)}`}
                  >
                    <RotateCcw size={12} />
                    <span className="mission-timeline__trajectory-label">Replay trajectory</span>
                    <span className="mission-timeline__trajectory-scope">{traj.label}</span>
                    <span className="mission-timeline__trajectory-status">{traj.status}</span>
                  </button>
                ))}
              </div>
            </div>
          )}

          {loading ? (
            <p className="mission-timeline__empty">Loading mission timeline…</p>
          ) : model.events.length === 0 ? (
            <p className="mission-timeline__empty">
              No mission events in the last 24h — dispatch an Agent task or wait for nightly drift scan.
            </p>
          ) : (
            <ol className="mission-timeline__list">
              {model.events.map(event => (
                <TimelineEventRow
                  key={event.id}
                  event={event}
                  onOpenAudit={onOpenAudit}
                  onOpenAgentDesk={onOpenAgentDesk}
                />
              ))}
            </ol>
          )}
        </div>
      )}
    </section>
  )
}
