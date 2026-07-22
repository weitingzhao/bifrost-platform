import { useState } from 'react'
import {
  Button,
  ConfirmDialog,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { postNetworkFirewallApply } from '@/api/network'
import type { NetworkAuditResponse } from '@/api/networkTypes'
import { OpsSection } from '@/components/layout/OpsSection'
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
  const qc = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [includeDefaultDeny, setIncludeDefaultDeny] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)

  const audit = liveProbe.audit
  const hasDrift = audit?.classification === 'POLICY_DRIFT'
  const canApply =
    canOperate && liveProbe.probeReach !== 'fail' && liveProbe.probeReach !== 'unknown' && hasDrift

  const applyMutation = useMutation({
    mutationFn: () => postNetworkFirewallApply({ include_default_deny: includeDefaultDeny }),
    onMutate: () => setActionMsg(null),
    onSuccess: resp => {
      setActionMsg(resp.message ?? 'Firewall apply completed')
      setConfirmOpen(false)
      void qc.invalidateQueries({ queryKey: ['network'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionMsg(err.message),
  })

  const gapCount = audit?.zone_binding_gaps?.length ?? 0
  const missingCount = audit?.missing_policies?.length ?? 0

  return (
    <OpsSection
      title="Firewall drift & apply"
      description="L0 audit via GET /api/v1/network/audit — L1 idempotent re-sync via POST /api/v1/network/firewall/apply (operator)."
      actions={
        canApply ? (
          <Button
            size="xs"
            variant="default"
            disabled={applyMutation.isPending}
            onClick={() => setConfirmOpen(true)}
          >
            {applyMutation.isPending ? 'Applying…' : 'Apply firewall'}
          </Button>
        ) : undefined
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

      {actionMsg != null && actionMsg !== '' && (
        <p
          className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
            applyMutation.isError ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
          }`}
        >
          {actionMsg}
        </p>
      )}

      <ConfirmDialog
        open={confirmOpen}
        title="Apply Bifrost firewall policies"
        message="Idempotent L1 apply — re-sync missing Bifrost zone-based firewall policies on the UCG controller. Does not toggle Default Security Posture or bulk-delete zones."
        bodyExtra={
          <label className="flex cursor-pointer items-center gap-2 text-[var(--text-dense-meta)]">
            <input
              type="checkbox"
              checked={includeDefaultDeny}
              onChange={e => setIncludeDefaultDeny(e.target.checked)}
            />
            Include default-deny rule (scripts/unifi_firewall_setup.py --include-default-deny)
          </label>
        }
        confirmLabel="Apply"
        confirming={applyMutation.isPending}
        onConfirm={() => applyMutation.mutate()}
        onCancel={() => setConfirmOpen(false)}
      />
    </OpsSection>
  )
}
