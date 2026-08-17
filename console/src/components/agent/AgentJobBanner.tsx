import { useState } from 'react'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { RemediationJob } from '@/api/remediationTypes'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useAgentJobLiveSession } from '@/hooks/useAgentJobLiveSession'
import {
  feedKindLabel,
  formatFeedEventLine,
} from '@/lib/agent/agentLiveFeed'
import { isBenignRemediationStreamError } from '@/lib/remediation/remediationJobDisplay'

interface AgentJobBannerProps {
  jobId: string
  /** Catalog label, e.g. "Platform · Release" */
  taskLabel?: string
  onDismiss: () => void
  /** Optional escape hatch — only inside expanded panel; never auto-navigates. */
  onOpenAgentDesk?: (jobId: string) => void
  onComplete?: (job: RemediationJob) => void
}

/** @deprecated Prefer AgentExecutionDock for shell chrome. Kept for inline/legacy embeds. */
export function AgentJobBanner({ jobId, taskLabel, onDismiss, onOpenAgentDesk, onComplete }: AgentJobBannerProps) {
  const [expanded, setExpanded] = useState(false)
  const {
    job,
    connected,
    error,
    isTerminal,
    pendingApproval,
    liveFeed,
    feedStats,
    recentEvents,
    elapsed,
    bannerVariant,
    statusLabel,
    respondPending,
    respond,
    reach,
  } = useAgentJobLiveSession(jobId, { onComplete, onDismiss })

  const connectionError =
    error != null && !isBenignRemediationStreamError(error) ? error : null

  const showInlineFeed = !isTerminal && liveFeed != null
  const showFeedPlaceholder = !isTerminal && liveFeed == null && connected
  const showStats = !isTerminal && (feedStats.toolCalls > 0 || feedStats.eventCount > 0)

  return (
    <div className={cn('agent-job-banner', `agent-job-banner--${bannerVariant}`, expanded && 'agent-job-banner--expanded')}>
      <div className="agent-job-banner__head">
        <StatusLamp value={reach} kind="reach" />
        <span className="agent-job-banner__chrome-kicker">Agent task</span>
        {taskLabel != null && taskLabel !== '' && (
          <span className="agent-job-banner__task-label">{taskLabel}</span>
        )}
        <span className="agent-job-banner__label">{statusLabel}</span>
        {showInlineFeed && liveFeed != null && (
          <div className="agent-job-banner__feed">
            <span className={cn('agent-job-banner__feed-kind', `agent-job-banner__feed-kind--${liveFeed.kind}`)}>
              {feedKindLabel(liveFeed.kind)}
            </span>
            <span className="agent-job-banner__feed-text" title={liveFeed.text}>
              {liveFeed.text}
            </span>
          </div>
        )}
        {showFeedPlaceholder && (
          <span className="agent-job-banner__feed-text agent-job-banner__feed-text--placeholder">
            Waiting for agent activity…
          </span>
        )}
        {showStats && (
          <span className="agent-job-banner__stats">
            {feedStats.toolCalls > 0 && `${feedStats.toolCalls} tool${feedStats.toolCalls === 1 ? '' : 's'}`}
            {feedStats.toolCalls > 0 && feedStats.eventCount > feedStats.toolCalls && ' · '}
            {feedStats.eventCount > feedStats.toolCalls && `${feedStats.eventCount} events`}
          </span>
        )}
        <AgentPhaseIndicator currentPhase={job?.phase} failed={job?.status === 'failed'} compact />
        {elapsed != null && !isTerminal && (
          <span className="agent-job-banner__elapsed">{elapsed}</span>
        )}
        {!connected && !isTerminal && error == null && (
          <span className="agent-job-banner__connecting">connecting…</span>
        )}
        <div className="agent-job-banner__actions">
          {!isTerminal && onOpenAgentDesk != null && (
            <Button variant="default" size="xs" onClick={() => onOpenAgentDesk(jobId)}>
              View agent →
            </Button>
          )}
          {!isTerminal && (
            <Button
              variant="outline"
              size="xs"
              onClick={() => setExpanded(v => !v)}
              aria-expanded={expanded}
            >
              {expanded ? <ChevronUp className="agent-job-banner__action-icon" aria-hidden /> : <ChevronDown className="agent-job-banner__action-icon" aria-hidden />}
              {expanded ? 'Hide' : 'Details'}
            </Button>
          )}
          {isTerminal && (
            <Button variant="outline" size="xs" onClick={onDismiss}>
              Dismiss
            </Button>
          )}
        </div>
      </div>

      {expanded && !isTerminal && (
        <div className="agent-job-banner__detail">
          <div className="agent-job-banner__detail-log dense-scroll-y">
            {recentEvents.length === 0 ? (
              <p className="agent-job-banner__detail-empty">Waiting for agent activity…</p>
            ) : (
              <ul className="agent-job-banner__detail-list">
                {recentEvents.map(ev => (
                  <li key={ev.id} className={cn('agent-job-banner__detail-item', `agent-job-banner__detail-item--${ev.type}`)}>
                    <span className="agent-job-banner__detail-type">{ev.type}</span>
                    <span className="agent-job-banner__detail-text">{formatFeedEventLine(ev)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {onOpenAgentDesk != null && (
            <div className="agent-job-banner__detail-footer">
              <Button variant="ghost" size="xs" onClick={() => onOpenAgentDesk(jobId)}>
                Open in Agent Desk
              </Button>
            </div>
          )}
        </div>
      )}

      {bannerVariant === 'done' && job?.summary != null && job.summary !== '' && (
        <p className="agent-job-banner__summary agent-job-banner__summary--done">{job.summary}</p>
      )}

      {bannerVariant === 'failed' && (
        <p className="agent-job-banner__summary agent-job-banner__summary--failed">
          {job?.error ?? job?.summary ?? 'Unknown error'}
        </p>
      )}

      {connectionError != null && !isTerminal && (
        <p className="agent-job-banner__summary agent-job-banner__summary--failed">
          Connection: {connectionError}
        </p>
      )}

      {pendingApproval != null && (
        <div className="agent-job-banner__approval">
          <RemediationApprovalBlock
            event={pendingApproval}
            compact
            submitting={respondPending}
            onRespond={(optionId, note, commitMessage) =>
              respond(optionId, note, commitMessage)
            }
          />
        </div>
      )}
    </div>
  )
}
