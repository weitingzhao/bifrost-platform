import { DenseTag, cn } from '@bifrost/ui'
import { STATE_WORD, TAG_VARIANT } from '@/lib/shell/stateWords'
import { SYSTEM_DOMAIN_ICON } from '@/lib/architecture/systemDomainCatalog'
import type { DomainHealth } from '@/domains/foundation/observability'

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
        'inline-flex items-center gap-1.5 rounded-[var(--control-radius)] border px-2 py-1 text-left transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-transparent bg-[var(--control-fill)] hover:bg-[var(--control-fill-hover)]',
      )}
    >
      <Icon className="size-3 shrink-0 text-muted-foreground" aria-hidden />
      <span className="shrink-0 whitespace-nowrap text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
      {/* Not a fault: boundary, the engine's word; what it means is in the sentence beside it. */}
      <DenseTag variant={TAG_VARIANT.boundary} className="shrink-0 whitespace-nowrap text-[9px]">
        {STATE_WORD.boundary}
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">
        by design · no runtime contract
      </span>
    </button>
  )
}
