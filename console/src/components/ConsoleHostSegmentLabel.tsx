import { cn } from '@bifrost/ui'
import type { ConsoleHost } from '@/api/console'
import { ConsoleHostBrandIcon } from '@/components/ConsoleHostBrandIcon'
import { ConsoleHostIpLabel } from '@/components/ConsoleHostIpLabel'
import {
  macAgentRoleLabel,
  type MacAgentHostRole,
} from '@/lib/agent/macHostRole'

function isLinuxConsoleHost(host: ConsoleHost): boolean {
  return host.group === 'linux' || host.group === 'compute'
}

function shortMacLabel(label: string): string {
  const hash = label.indexOf('#')
  if (hash >= 0) {
    const rest = label.slice(hash).trim()
    if (rest.length <= 14) return rest
  }
  const mini = label.match(/Mac Mini\s*#\d+/i)
  if (mini != null) return mini[0]
  return label.length > 16 ? `${label.slice(0, 15)}…` : label
}

export function ConsoleHostSegmentLabel({
  host,
  k8sNodeByIp,
  className,
  /** Dock chips: name + `.60` only (full IP in title). */
  dense = false,
  /** L-1 runner role from bridge — only set for bridge-identified hosts. */
  agentRole,
}: {
  host: ConsoleHost
  k8sNodeByIp?: Record<string, string>
  className?: string
  dense?: boolean
  agentRole?: MacAgentHostRole
}) {
  const k8sName = k8sNodeByIp?.[host.host]
  const linux = isLinuxConsoleHost(host)
  const roleLabel = agentRole != null ? macAgentRoleLabel(agentRole) : null
  const titleParts = [
    linux && k8sName != null ? k8sName : host.label,
    host.host,
    roleLabel != null ? `L-1 ${roleLabel}` : null,
    host.jump_label ? `via ${host.jump_label}` : null,
  ].filter((p): p is string => p != null && p !== '')
  const title = titleParts.join(' · ')

  return (
    <span
      className={cn(
        'inline-flex items-center',
        dense ? 'max-w-[9.5rem] gap-1' : 'max-w-[14rem] gap-1.5',
        className,
      )}
      title={title}
    >
      <ConsoleHostBrandIcon host={host} className={dense ? 'size-3 shrink-0' : undefined} />
      {linux && k8sName != null && (
        <span
          className={cn(
            'truncate font-medium text-foreground',
            dense ? 'text-dense-meta' : 'text-dense-caption',
          )}
        >
          {k8sName}
        </span>
      )}
      {host.group === 'mac' && (roleLabel != null || host.label !== host.host) && (
        <span
          className={cn(
            'truncate text-muted-foreground',
            dense ? 'text-dense-meta' : 'text-dense-caption',
            roleLabel != null && 'font-medium text-foreground',
          )}
        >
          {roleLabel ?? shortMacLabel(host.label)}
        </span>
      )}
      <ConsoleHostIpLabel
        ip={host.host}
        compact={!dense && (k8sName != null || host.group === 'mac')}
        octetOnly={dense}
      />
    </span>
  )
}
