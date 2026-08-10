import { OpsSection } from '@/components/layout/OpsSection'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'

export interface OpsDeskReleaseSectionProps {
  onNavigate: (tabId: string) => void
  rocketVerdict?: LaunchVerdict
  rocketCheckpoints?: LaunchCheckpoint[]
  satelliteVerdict?: LaunchVerdict
  satelliteCheckpoints?: LaunchCheckpoint[]
  pluginVerdict?: LaunchVerdict
  pluginCheckpoints?: LaunchCheckpoint[]
  onDispatchRelease?: () => void
  onDispatchTradeDeploy?: () => void
  onDispatchPluginLaunch?: () => void
  releasePending?: boolean
  tradeDeployPending?: boolean
  pluginLaunchPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  canDispatchPluginLaunch?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  pluginLaunchDisabledReason?: string
  onRocketAgentFix?: () => void
  onSatelliteAgentFix?: () => void
  rocketAgentFixPending?: boolean
  satelliteAgentFixPending?: boolean
  rocketAgentFixActive?: boolean
  satelliteAgentFixActive?: boolean
  rocketAgentFixDisabled?: boolean
  satelliteAgentFixDisabled?: boolean
  rocketAgentFixTitle?: string
  satelliteAgentFixTitle?: string
  onExpandAgentDock?: () => void
  onOpenAgentDesk?: () => void
  canOperate?: boolean
  releaseError?: string | null
  tradeDeployError?: string | null
  pluginLaunchError?: string | null
}

/**
 * Daily Ops Focus → Release: Mission Launch gate checklist restored.
 * Each lane shows LaunchGateBar checkpoints (why GO / NO-GO) + Agent launch CTA.
 */
export function OpsDeskReleaseSection({
  onNavigate,
  rocketVerdict,
  rocketCheckpoints = [],
  satelliteVerdict,
  satelliteCheckpoints = [],
  pluginVerdict,
  pluginCheckpoints = [],
  onDispatchRelease,
  onDispatchTradeDeploy,
  onDispatchPluginLaunch,
  releasePending = false,
  tradeDeployPending = false,
  pluginLaunchPending = false,
  canDispatchRelease = false,
  canDispatchTradeDeploy = false,
  canDispatchPluginLaunch = false,
  releaseDisabledReason,
  tradeDeployDisabledReason,
  pluginLaunchDisabledReason,
  onRocketAgentFix,
  onSatelliteAgentFix,
  rocketAgentFixPending = false,
  satelliteAgentFixPending = false,
  rocketAgentFixActive = false,
  satelliteAgentFixActive = false,
  rocketAgentFixDisabled = true,
  satelliteAgentFixDisabled = true,
  rocketAgentFixTitle,
  satelliteAgentFixTitle,
  onExpandAgentDock,
  onOpenAgentDesk,
  canOperate = false,
  releaseError,
  tradeDeployError,
  pluginLaunchError,
}: OpsDeskReleaseSectionProps) {
  return (
    <OpsSection
      id="task-cc-release-posture"
      title="Release"
      description="Rocket · Satellite · Plugin — checklist lamps show why launch is GO or blocked."
      actions={
        <button
          type="button"
          className="text-[var(--text-dense-caption)] text-primary hover:underline"
          onClick={() => onNavigate('platform-release')}
        >
          Launch Rocket →
        </button>
      }
      bodyPadding="compact"
    >
      <div className="flex flex-col gap-3">
        {releaseError != null && (
          <OpsFeedback variant="error" title="AI Release failed">
            {releaseError}
          </OpsFeedback>
        )}
        {tradeDeployError != null && (
          <OpsFeedback variant="error" title="AI Deploy Satellite failed">
            {tradeDeployError}
          </OpsFeedback>
        )}
        {pluginLaunchError != null && (
          <OpsFeedback variant="error" title="AI Launch Plugin failed">
            {pluginLaunchError}
          </OpsFeedback>
        )}

        {!canOperate && (
          <OpsFeedback variant="warning" title="Authenticate to dispatch">
            Use the header auth control before starting release Agent tasks. Auth is also a checklist lamp below.
          </OpsFeedback>
        )}

        <div className="grid grid-cols-1 gap-2 lg:grid-cols-3 lg:items-start">
          <OpsSection
            title="Rocket"
            description="Platform STG → PROD"
            bodyPadding="compact"
            className="min-w-0"
            actions={
              <button
                type="button"
                className="text-[var(--text-dense-caption)] text-primary hover:underline"
                onClick={() => onNavigate('platform-release')}
              >
                Detail →
              </button>
            }
          >
            {rocketVerdict != null ? (
              <LaunchGateBar
                layout="column"
                verdict={rocketVerdict}
                checkpoints={rocketCheckpoints}
                onAgentFix={onRocketAgentFix}
                agentFixPending={rocketAgentFixPending}
                agentFixActive={rocketAgentFixActive}
                agentFixDisabled={rocketAgentFixDisabled}
                agentFixTitle={rocketAgentFixTitle}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchRelease}
                launchLabel="AI Release"
                blockedLabel="Release blocked"
                launchPending={releasePending}
                launchDisabled={!canDispatchRelease}
                launchDisabledReason={releaseDisabledReason}
                onOpenDetail={() => onNavigate('platform-release')}
                detailLabel="Launch Rocket →"
                onOpenActiveRun={() => onNavigate('platform-release')}
                openActiveRunLabel="Open active run →"
              />
            ) : (
              <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                Loading rocket gate checklist…
              </p>
            )}
          </OpsSection>

          <OpsSection
            title="Satellite"
            description="Trade STG → PROD"
            bodyPadding="compact"
            className="min-w-0"
            actions={
              <button
                type="button"
                className="text-[var(--text-dense-caption)] text-primary hover:underline"
                onClick={() => onNavigate('trade-release')}
              >
                Detail →
              </button>
            }
          >
            {satelliteVerdict != null ? (
              <LaunchGateBar
                layout="column"
                verdict={satelliteVerdict}
                checkpoints={satelliteCheckpoints}
                onAgentFix={onSatelliteAgentFix}
                agentFixPending={satelliteAgentFixPending}
                agentFixActive={satelliteAgentFixActive}
                agentFixDisabled={satelliteAgentFixDisabled}
                agentFixTitle={satelliteAgentFixTitle}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchTradeDeploy}
                launchLabel="AI Deploy"
                blockedLabel="Deploy blocked"
                launchPending={tradeDeployPending}
                launchDisabled={!canDispatchTradeDeploy}
                launchDisabledReason={tradeDeployDisabledReason}
                onOpenDetail={() => onNavigate('trade-release')}
                detailLabel="Deploy Satellite →"
                onOpenActiveRun={() => onNavigate('trade-release')}
                openActiveRunLabel="Open active run →"
              />
            ) : (
              <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                Loading satellite gate checklist…
              </p>
            )}
          </OpsSection>

          <OpsSection
            title="Plugin · IB Gateway"
            description="make install — not Tekton"
            bodyPadding="compact"
            className="min-w-0"
            actions={
              <button
                type="button"
                className="text-[var(--text-dense-caption)] text-primary hover:underline"
                onClick={() => onNavigate('plugin-release')}
              >
                Detail →
              </button>
            }
          >
            {pluginVerdict != null ? (
              <LaunchGateBar
                layout="column"
                verdict={pluginVerdict}
                checkpoints={pluginCheckpoints}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchPluginLaunch}
                launchLabel="AI Launch Plugin"
                blockedLabel="Plugin launch blocked"
                launchPending={pluginLaunchPending}
                launchDisabled={!canDispatchPluginLaunch}
                launchDisabledReason={pluginLaunchDisabledReason}
                onOpenDetail={() => onNavigate('plugin-release')}
                detailLabel="Plugin Release →"
                onOpenActiveRun={() => onNavigate('plugin-release')}
                openActiveRunLabel="Open active run →"
              />
            ) : (
              <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                Loading plugin gate checklist…
              </p>
            )}
          </OpsSection>
        </div>
      </div>
    </OpsSection>
  )
}
