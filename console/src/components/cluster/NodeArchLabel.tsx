import { cn, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { Cpu, Server } from 'lucide-react'

type ArchKind = 'arm64' | 'amd64' | 'other'

function normalizeArch(arch: string | undefined): ArchKind | null {
  if (arch == null || arch === '' || arch === '—') return null
  const v = arch.toLowerCase()
  if (v === 'arm64' || v === 'arm') return 'arm64'
  if (v === 'amd64' || v === 'x86_64' || v === 'x64') return 'amd64'
  return 'other'
}

const ARCH_META: Record<
  Exclude<ArchKind, 'other'>,
  { label: string; hint: string; Icon: typeof Cpu; iconClass: string }
> = {
  arm64: {
    label: 'arm64',
    hint: 'ARM64 — edge / agent nodes',
    Icon: Cpu,
    iconClass: 'text-amber-500',
  },
  amd64: {
    label: 'amd64',
    hint: 'AMD64 — CI builds and general runtime',
    Icon: Server,
    iconClass: 'text-sky-500',
  },
}

export function NodeArchLabel({
  arch,
  showTooltip = true,
  className,
}: {
  arch?: string
  showTooltip?: boolean
  className?: string
}) {
  const kind = normalizeArch(arch)
  if (kind == null) {
    return <span className={cn('text-[var(--muted-foreground)]', className)}>—</span>
  }
  if (kind === 'other') {
    return <span className={cn('font-mono-tabular', className)}>{arch}</span>
  }

  const meta = ARCH_META[kind]
  const { Icon, iconClass, label, hint } = meta

  const content = (
    <span className={cn('inline-flex items-center gap-1.5 font-mono-tabular', className)}>
      <Icon className={cn('size-3.5 shrink-0', iconClass)} aria-hidden />
      <span>{label}</span>
    </span>
  )

  if (!showTooltip) return content

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex">{content}</span>
      </TooltipTrigger>
      <TooltipContent side="top">{hint}</TooltipContent>
    </Tooltip>
  )
}
