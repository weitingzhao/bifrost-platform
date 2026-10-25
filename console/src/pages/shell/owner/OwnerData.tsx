import { Dot } from './OwnerParts'
import { TONE_COLOR } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

/** Data: five tiles, each with its state, one figure, one line, and its detail page. */
export function OwnerData({ data, infrastructure = false }: { data: OwnerConsoleData; infrastructure?: boolean }) {
  const tiles = data.tiles.filter(tile => ['Backups', 'Test copies'].includes(tile.name) === infrastructure)
  const title = infrastructure ? 'Storage and copies' : 'Application data'
  return <section aria-label={title} className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">{title}</h2>
    <ul className="m-0 grid list-none gap-3 p-0 @xl:grid-cols-2 @5xl:grid-cols-3">{tiles.map(tile => <li key={tile.name}>
      <a href={tile.href} aria-label={`${tile.name}: ${tile.state}`} className="flex h-full min-w-0 flex-col gap-1.5 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-4 text-foreground no-underline">
        <span className="flex items-center justify-between gap-2"><span className="font-semibold">{tile.name}</span>
          <span className="flex items-center gap-1.5 text-xs font-bold" style={{ color: TONE_COLOR[tile.tone] }}><Dot tone={tile.tone} size="size-2" />{tile.state}</span></span>
        <span className="break-words text-xl font-bold">{tile.figure}</span>
        <span className="break-words text-xs text-muted-foreground">{tile.detail}</span>
      </a>
    </li>)}</ul>
  </section>
}
