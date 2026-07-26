import { cn } from '@bifrost/ui'
import { BookOpen } from 'lucide-react'
import { GOVERNANCE_MENU_GROUPS } from '@/lib/consoleNavConfig'

export type GuidesSettingsNavProps = {
  activeTab: string
  onSelect: (tabId: string) => void
  className?: string
}

/**
 * Settings-style secondary nav for Governance / Guides docs.
 * Shown only while a Governance plane tab is active — keeps User menu to one entry.
 */
export function GuidesSettingsNav({
  activeTab,
  onSelect,
  className,
}: GuidesSettingsNavProps) {
  return (
    <nav
      className={cn(
        'flex w-52 shrink-0 flex-col gap-3 border-r border-border pr-3',
        className,
      )}
      aria-label="Guides"
    >
      <div className="flex items-center gap-1.5 px-1">
        <BookOpen size={14} className="text-muted-foreground" aria-hidden />
        <span className="text-[var(--text-dense-label)] font-semibold tracking-tight">
          Guides
        </span>
      </div>
      {GOVERNANCE_MENU_GROUPS.map(group => (
        <div key={group.label} className="flex flex-col gap-0.5">
          <p className="m-0 px-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
            {group.label}
          </p>
          {group.items.map(item => {
            const Icon = item.icon
            const active = item.id === activeTab
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => onSelect(item.id)}
                className={cn(
                  'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[var(--text-dense-caption)] transition-colors',
                  active
                    ? 'bg-secondary font-semibold text-foreground'
                    : 'text-muted-foreground hover:bg-secondary/60 hover:text-foreground',
                )}
              >
                <Icon size={13} className="shrink-0 opacity-80" aria-hidden />
                <span className="truncate">{item.label}</span>
              </button>
            )
          })}
        </div>
      ))}
    </nav>
  )
}
