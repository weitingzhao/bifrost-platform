import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { StackAddonsResponse } from '@/api/types'
import { installStackAddon, upgradeStackAddon } from '@/api/platform'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { WizardProcedureSteps } from '@/components/cluster/WizardProcedureSteps'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { NodeWizardStep } from '@/lib/cluster/nodeWizard'
import {
  currentStackWizardStep,
  stackInstallComplete,
  stackInstallWizardSteps,
  type StackWizardAction,
} from '@/lib/delivery/stackWizard'

interface StackInstallWizardPanelProps {
  data: StackAddonsResponse | undefined
  isLoading: boolean
  errorMessage?: string | null
  layout?: 'operate' | 'observe'
}

function actionLabel(action: StackWizardAction): string {
  return action === 'upgrade' ? 'Upgrade add-on' : 'Install add-on'
}

export function StackInstallWizardPanel({
  data,
  isLoading,
  errorMessage,
  layout = 'observe',
}: StackInstallWizardPanelProps) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const addons = data?.addons ?? []
  const steps = useMemo(() => stackInstallWizardSteps(addons), [addons])
  const current = currentStackWizardStep(steps)
  const complete = stackInstallComplete(addons)

  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmAction, setConfirmAction] = useState<{
    addonId: string
    label: string
    action: StackWizardAction
  } | null>(null)

  const installMutation = useMutation({
    mutationFn: installStackAddon,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['stack', 'addons'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
      setActionError(null)
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const upgradeMutation = useMutation({
    mutationFn: upgradeStackAddon,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['stack', 'addons'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setConfirmAction(null)
      setActionError(null)
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const pending = installMutation.isPending || upgradeMutation.isPending

  const headerExtra = (
    <>
      {errorMessage != null && errorMessage !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorMessage}</p>
      )}
      {!isLoading && data != null && errorMessage == null && (
        <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
          <StatusLamp value={complete ? 'ok' : data.reachability} kind="reach" />
          <span>
            {complete
              ? 'CI/CD stack complete — Registry, Gitea, and Tekton ready'
              : 'Install stack add-ons in order: Registry → Gitea → Tekton (admin token)'}
          </span>
          {complete && <DenseTag variant="success">Complete</DenseTag>}
        </p>
      )}
      {!canAdmin && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Admin token required to install or upgrade stack add-ons.
        </p>
      )}
      {actionError != null && actionError !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{actionError}</p>
      )}
    </>
  )

  return (
    <OpsSection
      title={layout === 'operate' ? 'Install CI/CD stack' : 'CI/CD stack install wizard'}
      leading={<DeliveryBrandIcon id="tekton" variant="scope" />}
      actions={
        layout === 'operate' ? undefined : (
          <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            POST /api/v1/stack/addons/&#123;name&#125;/install
          </span>
        )
      }
      headerExtra={headerExtra}
      bodyPadding="default"
      overflow="visible"
    >
      <WizardProcedureSteps steps={steps as NodeWizardStep[]} flowLabel="Stack install" />

      {current != null && current.action != null && (
        <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-[var(--border)] pt-3">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Next: <strong className="text-[var(--foreground)]">{current.label}</strong>
          </span>
          <Button
            size="sm"
            disabled={!canAdmin || pending || isLoading}
            onClick={() =>
              setConfirmAction({
                addonId: current.addonId,
                label: current.label,
                action: current.action!,
              })
            }
          >
            {pending ? 'Running…' : actionLabel(current.action)}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={confirmAction != null}
        title={confirmAction?.action === 'upgrade' ? 'Upgrade stack add-on' : 'Install stack add-on'}
        message={
          confirmAction != null
            ? `${confirmAction.action === 'upgrade' ? 'Re-run the upgrade script for' : 'Install'} ${confirmAction.label}? Scripts run on the platform-api host against the cluster kubeconfig. This may take several minutes.`
            : ''
        }
        confirmLabel={confirmAction?.action === 'upgrade' ? 'Upgrade' : 'Install'}
        confirming={pending}
        onConfirm={() => {
          if (confirmAction == null) return
          if (confirmAction.action === 'upgrade') {
            upgradeMutation.mutate(confirmAction.addonId)
            return
          }
          installMutation.mutate(confirmAction.addonId)
        }}
        onCancel={() => setConfirmAction(null)}
      />
    </OpsSection>
  )
}
