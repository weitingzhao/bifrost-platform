import { cn } from '@bifrost/ui'
import { Box } from 'lucide-react'
import type { SimpleIcon } from 'simple-icons'
import { siApple, siUbuntu } from 'simple-icons'

import type { ConsoleHost } from '@/api/console'

type HostBrandKind = 'apple' | 'ubuntu' | 'ubuntu-vm' | null

/** Mac-hosted Linux VM (UTM Ubuntu on Mac Mini). */
function isMacHostedUbuntuVm(host: ConsoleHost): boolean {
  return host.group === 'linux' && host.id.endsWith('-orb')
}

function resolveHostBrandKind(host: ConsoleHost): HostBrandKind {
  if (isMacHostedUbuntuVm(host)) {
    return 'ubuntu-vm'
  }
  if (host.group === 'linux' || host.id.startsWith('mini-pc')) {
    return 'ubuntu'
  }
  if (host.group === 'mac' || host.id.startsWith('mac-mini')) {
    return 'apple'
  }
  return null
}

function BrandSvg({ icon, className }: { icon: SimpleIcon; className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" role="img" aria-label={icon.title}>
      <path d={icon.path} fill={icon.slug === 'apple' ? 'currentColor' : `#${icon.hex}`} />
    </svg>
  )
}

function UbuntuVmBrandIcon({ className = 'size-3.5 shrink-0' }: { className?: string }) {
  return (
    <span
      className={cn('relative inline-flex shrink-0 items-center justify-center', className)}
      title="Ubuntu VM"
      aria-label="Ubuntu VM"
    >
      <BrandSvg icon={siUbuntu} className="size-full" />
      <span className="absolute -bottom-px -right-px flex size-[9px] items-center justify-center rounded-[2px] bg-[var(--color-surface-elevated)] ring-1 ring-[var(--border)]">
        <Box className="size-[7px] stroke-[2.5] text-foreground" aria-hidden />
      </span>
    </span>
  )
}

export type ConsoleHostBrandIconProps = {
  host: ConsoleHost
  className?: string
}

/** Brand mark for Server Console host segments (Apple = Mac, Ubuntu = Linux, Ubuntu+Box = Mac-hosted VM). */
export function ConsoleHostBrandIcon({ host, className = 'size-3.5 shrink-0' }: ConsoleHostBrandIconProps) {
  const kind = resolveHostBrandKind(host)
  if (kind === 'ubuntu-vm') {
    return <UbuntuVmBrandIcon className={className} />
  }
  if (kind === 'ubuntu') {
    return <BrandSvg icon={siUbuntu} className={className} />
  }
  if (kind === 'apple') {
    return <BrandSvg icon={siApple} className={className} />
  }
  return null
}
