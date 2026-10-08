import { useState } from 'react'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { NetworkAuditResponse } from '@/api/networkTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { RequestFirewallApply } from '@/components/shell/clusterActionRequests'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function classificationVariant(
  classification: NetworkAuditResponse['classification'],
): 'success' | 'warning' | 'neutral' {
  if (classification === 'POLICY_NOMINAL') return 'success'
  if (classification === 'POLICY_DRIFT') return 'warning'
  return 'neutral'
}

export function NetworkFirewallPanel() {
  const liveProbe = useNetworkLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [includeDefaultDeny, setIncludeDefaultDeny] = useState(false)

  const audit = liveProbe.audit
  const hasDrift = audit?.classification === 'POLICY_DRIFT'
  const canApply =
    canOperate && liveProbe.probeReach !== 'fail' && liveProbe.probeReach !== 'unknown' && hasDrift

  const gapCount = audit?.zone_binding_gaps?.length ?? 0
  const missingCount = audit?.missing_policies?.length ?? 0

  return (
    <OpsSection
      title="Firewall drift & apply"
      description="L0 audit via GET /api/v1/network/audit — L1 idempotent re-sync via POST /api/v1/network/firewall/apply (operator)."
      actions={
        canApply ? <RequestFirewallApply includeDefaultDeny={includeDefaultDeny} /> : undefined
      }
      bodyPadding="default"
    >
      <div className="flex flex-wrap items-center gap-2">
        <StatusLamp value={liveProbe.probeReach} kind="reach" />
        <DenseTag variant={classificationVariant(audit?.classification)}>
          {audit?.classification ?? (liveProbe.isLoading ? 'AUDIT…' : 'UNKNOWN')}
        </DenseTag>
        <DenseTag variant="info">L1 apply</DenseTag>
        <code className="text-[var(--text-dense-caption)] font-mono">
          POST /api/v1/network/firewall/apply
        </code>
      </div>

      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        {liveProbe.isLoading
          ? 'Loading audit…'
          : audit?.error != null && audit.error !== ''
            ? audit.hint ?? audit.error
            : hasDrift
              ? `${gapCount} zone gap(s) · ${missingCount} missing policy(ies) — apply re-syncs Bifrost ZBF rules from FIREWALL_RULES catalog (Session v2).`
              : audit?.classification === 'POLICY_NOMINAL'
                ? 'Policy nominal — no L1 apply needed.'
                : liveProbe.status?.reachable !== true
                  ? 'UniFi probe unreachable — configure UNIFI_HOST/USER/PASS on platform-api before apply.'
                  : 'Audit pending or inconclusive.'}
      </p>

      {audit?.bifrost_policy_count != null && audit.expected_policy_count != null && (
        <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Policies on controller: {audit.bifrost_policy_count}/{audit.expected_policy_count} expected
        </p>
      )}

      {!canOperate && hasDrift && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--warning)]">
          Operator authentication required to apply firewall changes.
        </p>
      )}

      {canApply ? (
        <label className="mt-2 flex cursor-pointer items-center gap-2 text-[var(--text-dense-meta)]">
          <input
            type="checkbox"
            checked={includeDefaultDeny}
            onChange={e => setIncludeDefaultDeny(e.target.checked)}
          />
          Include default-deny rule
        </label>
      ) : null}
    </OpsSection>
  )
}
