import { Button, cn } from '@bifrost/ui'
import { Loader2, Sparkles } from 'lucide-react'

export interface AgentTriggerButtonProps {
  /** Button label when idle, e.g. "AI Fix", "AI Release" */
  label: string
  pending?: boolean
  pendingLabel?: string
  /** Ambient job already running — keep affordance visible (not a dead disabled control). */
  active?: boolean
  activeLabel?: string
  disabled?: boolean
  /** Shown as native title tooltip when disabled or on hover */
  title?: string
  onClick: () => void
  size?: 'default' | 'sm' | 'xs'
  className?: string
}

/**
 * Unified Ops Console control for starting an ambient Agent task.
 * Same visual language on Operator Plane, Launch Rocket, Control Room, etc.
 */
export function AgentTriggerButton({
  label,
  pending = false,
  pendingLabel = 'Starting…',
  active = false,
  activeLabel = 'Running…',
  disabled = false,
  title,
  onClick,
  size = 'sm',
  className,
}: AgentTriggerButtonProps) {
  const busy = pending || active
  /** Disabled must not look like a ready green CTA (Launch No-Go mental model). */
  const variant = disabled && !busy ? 'outline' : active && !pending ? 'outline' : 'default'
  return (
    <Button
      type="button"
      variant={variant}
      size={size}
      disabled={disabled || pending}
      title={title}
      onClick={onClick}
      className={cn('agent-trigger-btn', className)}
    >
      {busy ? (
        <>
          <Loader2 className="agent-trigger-btn__icon animate-spin" aria-hidden />
          {pending ? pendingLabel : activeLabel}
        </>
      ) : (
        <>
          <Sparkles className="agent-trigger-btn__icon" aria-hidden />
          {label}
        </>
      )}
    </Button>
  )
}
