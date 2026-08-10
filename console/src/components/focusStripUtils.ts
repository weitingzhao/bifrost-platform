import type { DenseTagVariant } from '@bifrost/ui'

const STATUS_VARIANT: Record<string, DenseTagVariant> = {
  CLOSED: 'neutral',
  SIGNED: 'success',
  IN_PROGRESS: 'info',
  BLOCKED_ON: 'danger',
  NOT_STARTED: 'neutral',
  DEPLOYED: 'success',
}

export function milestoneStatusVariant(status: string): DenseTagVariant {
  return STATUS_VARIANT[status] ?? 'category'
}

export function flywheelLabel(code: string): string {
  if (code === 'A') return 'Flywheel A'
  if (code === 'B') return 'Flywheel B'
  return code
}
