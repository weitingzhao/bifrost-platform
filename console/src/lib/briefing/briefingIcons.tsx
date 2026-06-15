import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  Archive,
  ArrowRightLeft,
  Bug,
  Container,
  Hammer,
  LayoutDashboard,
  PackageCheck,
  Rocket,
  Server,
  Shield,
  Workflow,
} from 'lucide-react'
import type { LaneId } from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'

export const TRACK_ICONS: Record<TrackId, LucideIcon> = {
  build: Hammer,
  migrate: ArrowRightLeft,
  operate: Activity,
}

export const LANE_ICONS: Record<LaneId, LucideIcon> = {
  'console-api': LayoutDashboard,
  'cluster-infra': Server,
  'mcp-gitops': Workflow,
  'compose-k3s': Container,
  'legacy-retire': Archive,
  'trade-stack': PackageCheck,
  governance: Shield,
  troubleshoot: Bug,
  release: Rocket,
}

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
