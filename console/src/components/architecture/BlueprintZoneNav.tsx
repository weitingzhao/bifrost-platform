import { DenseTag } from '@bifrost/ui'
import { BLUEPRINT_ZONE_NAV } from '@/lib/architecture/blueprintZones'

export function BlueprintZoneNav() {
  return (
    <nav
      aria-label="Blueprint governance zones"
      className="page-section panel-elevated flex flex-wrap items-center gap-2 px-4 py-3"
    >
      <span className="briefing-section-kicker m-0 shrink-0">Jump to zone</span>
      {BLUEPRINT_ZONE_NAV.map(item => (
        <a
          key={item.id}
          href={`#${item.anchor}`}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] bg-[var(--background)] px-2.5 py-1 text-[var(--text-dense-meta)] no-underline transition-colors hover:bg-[var(--secondary)]"
        >
          <DenseTag variant={item.id === 'constitution' ? 'category' : item.id === 'spine' ? 'warning' : 'neutral'}>
            {item.layer}
          </DenseTag>
        </a>
      ))}
    </nav>
  )
}
