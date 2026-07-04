import { Button, cn } from '@bifrost/ui'
import { Loader2, Sparkles } from 'lucide-react'

export interface AgentTriggerButtonProps {
  /** Button label when idle, e.g. "AI Fix", "AI Release" */
  label: string
  pending?: boolean
  pendingLabel?: string
  disabled?: boolean
  /** Shown as native title tooltip when disabled or on hover */
  title?: string
  onClick: () => void
  size?: 'default' | 'sm' | 'xs'
  className?: string
}

/**
 * Unified Ops Console control for starting an ambient Agent task.
 * Same visual language on Operator Plane, Platform Release, Control Room, etc.
 */
export function AgentTriggerButton({
  label,
  pending = false,
  pendingLabel = 'Starting…',
  disabled = false,
  title,
  onClick,
  size = 'sm',
  className,
}: AgentTriggerButtonProps) {
  return (
    <Button
      type="button"
      variant="default"
      size={size}
      disabled={disabled || pending}
      title={title}
      onClick={onClick}
      className={cn('agent-trigger-btn', className)}
    >
      {pending ? (
        <>
          <Loader2 className="agent-trigger-btn__icon animate-spin" aria-hidden />
          {pendingLabel}
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
