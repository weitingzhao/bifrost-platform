import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  Archive,
  ArrowRightLeft,
  Bot,
  Boxes,
  Bug,
  CandlestickChart,
  Container,
  Database,
  GitBranch,
  GitPullRequestArrow,
  Globe,
  Hammer,
  HeartPulse,
  LayoutDashboard,
  LineChart,
  Monitor,
  Network,
  PackageCheck,
  Rocket,
  ScanSearch,
  Server,
  Shield,
  Sparkles,
  Unplug,
  Wifi,
  Workflow,
} from 'lucide-react'
import type { LaneId } from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'

export const TRACK_ICONS: Record<TrackId, LucideIcon> = {
  build: Hammer,
  migrate: ArrowRightLeft,
  automate: Bot,
  infra: Network,
  operate: Activity,
}

const LANE_ICON_MAP: Partial<Record<LaneId, LucideIcon>> = {
  'console-api': LayoutDashboard,
  'cluster-infra': Server,
  'mcp-gitops': Workflow,
  'cicd-delivery': GitPullRequestArrow,
  'compose-k3s': Container,
  'trade-k8s-native': Boxes,
  'data-layer-k3s': Database,
  'legacy-retire': Archive,
  'trade-stack': PackageCheck,
  'platform-gitops': GitBranch,
  'agent-infra': Unplug,
  'drift-remediation': ScanSearch,
  'agent-services': Bot,
  'network-server': Server,
  'network-wifi': Wifi,
  'ai-network': Globe,
  governance: Shield,
  troubleshoot: Bug,
  release: Rocket,
  'business-advisory': LineChart,
  'platform-health': HeartPulse,
  'trade-features': Sparkles,
  'network-monitoring': Monitor,
  'polygon-vendor': CandlestickChart,
  'ib-vendor': Unplug,
  'vendor-health': Activity,
}

/** Icon for a lane id — known lanes mapped; unknown lanes fall back to LayoutDashboard. */
export function laneIcon(id: LaneId): LucideIcon {
  return LANE_ICON_MAP[id] ?? LayoutDashboard
}

/** @deprecated Prefer laneIcon(id) — kept for existing JSX keyed access. */
export const LANE_ICONS: Record<string, LucideIcon> = new Proxy(
  {} as Record<string, LucideIcon>,
  { get: (_t, prop: string) => laneIcon(prop) },
)

interface BriefingIconBadgeProps {
  icon: LucideIcon
  selected?: boolean
  size?: 'sm' | 'md'
}

export function BriefingIconBadge({ icon: Icon, selected = false, size = 'md' }: BriefingIconBadgeProps) {
  const box = size === 'sm' ? 'h-7 w-7' : 'h-8 w-8'
  const glyph = size === 'sm' ? 'h-3.5 w-3.5' : 'h-4 w-4'

  return (
    <span
      className={[
        'flex shrink-0 items-center justify-center rounded-md transition-colors',
        box,
        selected
          ? 'bg-[var(--primary)] text-[var(--primary-foreground)]'
          : 'bg-[var(--border)]/60 text-[var(--muted-foreground)]',
      ].join(' ')}
    >
      <Icon className={glyph} strokeWidth={2} aria-hidden />
    </span>
  )
}
