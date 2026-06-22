import { cn } from '@bifrost/ui'
import type { ConsoleHost } from '@/api/console'
import { ConsoleHostBrandIcon } from '@/components/ConsoleHostBrandIcon'
import { ConsoleHostIpLabel } from '@/components/ConsoleHostIpLabel'

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
}: {
  host: ConsoleHost
  k8sNodeByIp?: Record<string, string>
  className?: string
}) {
  const k8sName = k8sNodeByIp?.[host.host]
  const linux = isLinuxConsoleHost(host)
  const title =
    linux && k8sName != null
      ? `${k8sName} · ${host.host}`
      : host.jump_label
        ? `${host.label} · ${host.host} · via ${host.jump_label}`
        : `${host.label} · ${host.host}`

  return (
    <span
      className={cn('inline-flex max-w-[14rem] items-center gap-1.5', className)}
      title={title}
    >
      <ConsoleHostBrandIcon host={host} />
      {linux && k8sName != null && (
        <span className="truncate text-dense-caption font-medium text-foreground">{k8sName}</span>
      )}
      {host.group === 'mac' && host.label !== host.host && (
        <span className="truncate text-dense-caption text-muted-foreground">
          {shortMacLabel(host.label)}
        </span>
      )}
      <ConsoleHostIpLabel ip={host.host} compact={k8sName != null || host.group === 'mac'} />
    </span>
  )
}
