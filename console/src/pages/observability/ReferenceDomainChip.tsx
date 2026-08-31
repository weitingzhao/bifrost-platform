import { DenseTag, cn } from '@bifrost/ui'
import { SYSTEM_DOMAIN_ICON } from '@/lib/architecture/systemDomainCatalog'
import type { DomainHealth } from '@/lib/observability'

/** Compact selectable chip for Apollo reference planes (no runtime probe contract). */
export function ReferenceDomainChip({
  domain,
  selected,
  onSelect,
}: {
  domain: DomainHealth
  selected: boolean
  onSelect: () => void
}) {
  const Icon = SYSTEM_DOMAIN_ICON[domain.domain]
  return (
    <button
      type="button"
      onClick={onSelect}
      title={`${domain.label} — by design · no runtime contract`}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-left transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)]/70 hover:bg-[var(--accent)]/50',
      )}
    >
      <Icon className="size-3 shrink-0 text-muted-foreground" aria-hidden />
      <span className="text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
      <DenseTag variant="neutral" className="text-[9px]">
        reference
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">
        by design · no runtime contract
      </span>
    </button>
  )
}
