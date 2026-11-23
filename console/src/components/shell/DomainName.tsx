import { Glyph } from '@bifrost/ui/city/worldGlyphs'
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'

/**
 * A domain's name with its world glyph (箭10 2026-11-23, World's gap table item 6: the same domain wears the same
 * icon on every view). The glyph comes from the one table the City's tech tree and Signals read; a name the table
 * does not know shows no glyph rather than a made-up one.
 */
export function DomainName({ id, className }: { id: string; className?: string }) {
  const glyph = DOMAIN_GLYPH[id]
  return (
    <span className={`inline-flex min-w-0 items-center gap-1 ${className ?? ''}`} data-domain-name={id}>
      {glyph ? <span className="shrink-0 text-muted-foreground" aria-hidden><Glyph name={glyph} size={14} /></span> : null}
      <span className="min-w-0 truncate">{id}</span>
    </span>
  )
}
