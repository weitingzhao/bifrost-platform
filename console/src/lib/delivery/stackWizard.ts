import type { StackAddonView } from '@/api/types'
import type { WizardStepStatus } from '@/lib/cluster/nodeWizard'

export const STACK_INSTALL_ORDER = ['registry', 'gitea', 'tekton'] as const

export type StackAddonId = (typeof STACK_INSTALL_ORDER)[number]

export type StackWizardAction = 'install' | 'upgrade'

export interface StackWizardStep {
  id: string
  label: string
  description: string
  status: WizardStepStatus
  action?: StackWizardAction
  addonId: StackAddonId
}

function addonById(addons: StackAddonView[], id: StackAddonId): StackAddonView | undefined {
  return addons.find(a => a.id === id)
}

function stepStatus(addon: StackAddonView | undefined): WizardStepStatus {
  if (addon == null) return 'pending'
  if (addon.status === 'installed' && addon.reachability === 'ok') return 'done'
  if (addon.status === 'not_installed') return 'pending'
  return 'current'
}

function installDescription(id: StackAddonId): string {
  switch (id) {
    case 'registry':
      return 'Internal NodePort registry (:30500) in cicd namespace.'
    case 'gitea':
      return 'Persistent Gitea Git server for Tekton clone sources.'
    case 'tekton':
      return 'Tekton Pipelines controller + bifrost-smoke and deliver-stg manifests.'
    default:
      return 'Install stack add-on via platform-api (admin).'
  }
}

export function stackInstallWizardSteps(addons: StackAddonView[]): StackWizardStep[] {
  const steps: StackWizardStep[] = STACK_INSTALL_ORDER.map(id => {
    const addon = addonById(addons, id)
    const label = addon?.label ?? id
    return {
      id,
      addonId: id,
      label,
      description: installDescription(id),
      status: 'pending' as WizardStepStatus,
    }
  })

  let foundCurrent = false
  for (let i = 0; i < steps.length; i++) {
    const addon = addonById(addons, steps[i].addonId)
    const status = stepStatus(addon)
    if (status === 'done') {
      steps[i].status = 'done'
      continue
    }
    if (!foundCurrent) {
      steps[i].status = 'current'
      steps[i].action = addon?.status === 'installed' ? 'upgrade' : 'install'
      foundCurrent = true
      continue
    }
    steps[i].status = 'pending'
  }

  if (!foundCurrent && steps.length > 0) {
    steps[steps.length - 1].status = 'done'
  }

  return steps
}

export function currentStackWizardStep(steps: StackWizardStep[]): StackWizardStep | null {
  return steps.find(s => s.status === 'current') ?? null
}

export function stackInstallComplete(addons: StackAddonView[]): boolean {
  return STACK_INSTALL_ORDER.every(id => {
    const addon = addonById(addons, id)
    return addon?.status === 'installed' && addon.reachability === 'ok'
  })
}

/** True when Operate/Observe should show the install wizard (action or remediation needed). */
export function stackNeedsOperatePanel(addons: StackAddonView[]): boolean {
  if (addons.length === 0) return false
  if (!stackInstallComplete(addons)) return true
  return addons.some(a => a.status === 'degraded' || a.reachability === 'fail')
}
