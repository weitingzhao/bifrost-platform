/**
 * Three-tone visual language for Briefing:
 * - Highlight: only Selected Lane (primary border + primary tint)
 * - White: in-filter / focus-path controls (card surface)
 * - Gray: out-of-focus controls (muted, lowered contrast)
 */

/** Digest status tiles — selected = White + left primary rail; unselected = Gray. */
export function briefingDigestTileClass(selected: boolean): string {
  return [
    'box-border flex h-[4.75rem] flex-col justify-center overflow-hidden rounded-[var(--card-radius)] border px-3 py-2 text-left transition-colors',
    selected
      ? 'border-transparent border-l-2 border-l-[var(--primary)] bg-[var(--card-fill-hover)]'
      : 'border-[var(--card-border)] bg-[var(--card-fill)] opacity-70 hover:opacity-90',
  ].join(' ')
}

/**
 * Compact Scope Line chip (wide layouts) — icon + label + r/p/d.
 * White/Gray shell; when active, underline uses View accent (`data-task-mode` → `--task-mode-accent`).
 */
export function briefingScopeLineChipClass(active: boolean): string {
  return [
    'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-[var(--control-radius)] border px-2 text-left transition-colors',
    active
      ? 'border-transparent border-b-2 border-b-[var(--task-mode-accent)] bg-[var(--control-fill-hover)]'
      : 'border-transparent bg-[var(--control-fill)] opacity-80 hover:opacity-100 hover:bg-[var(--control-fill-hover)]',
  ].join(' ')
}

/**
 * Full-width Scope Line row for narrow master pane — [icon · name] …… [r/p/d].
 * Active = accent left rail + tinted icon/label; idle = muted flat row.
 */
export function briefingScopeLineRowClass(active: boolean): string {
  return [
    'flex h-7 w-full items-center gap-1.5 rounded-[var(--control-radius)] border px-1.5 text-left transition-colors',
    active
      ? 'border-transparent border-l-2 border-l-[var(--task-mode-accent)] bg-[var(--control-fill)]'
      : 'border-transparent bg-transparent opacity-80 hover:opacity-100 hover:bg-[var(--control-fill)]',
  ].join(' ')
}

/**
 * Compact 2-col grid cell for Scope Line picker.
 * Grid container uses `gap-px` on the page ground, so the gaps read as hairline dividers.
 * Active = accent bottom bar + card surface; idle = muted surface.
 * `span2` makes the cell span both columns (used for "All").
 */
export function briefingScopeGridCellClass(active: boolean, span2 = false): string {
  return [
    'flex items-center gap-1.5 px-2 py-1.5 text-left transition-colors',
    span2 ? 'col-span-2' : '',
    active
      ? 'bg-[var(--card-fill-hover)] shadow-[inset_0_-2px_0_var(--task-mode-accent)]'
      : 'bg-[var(--card-fill)] hover:bg-[var(--card-fill-hover)]',
  ].join(' ')
}

/**
 * r/p/d digit colors — Ready / Planned / Doing (maturity order).
 * Always distinct, even when the digit is zero.
 */
export const BRIEFING_DPR_COLOR = {
  ready: 'text-[var(--color-env-dev)]',
  planned: 'text-[var(--color-env-stg)]',
  doing: 'text-[var(--color-lamp-yellow)]',
} as const

/** Scope chip (All / Line) — active = White; inactive = Gray. No primary fill. */
export function briefingScopeChipClass(active: boolean): string {
  return [
    'inline-flex h-8 items-center gap-1.5 rounded-[var(--control-radius)] border px-2.5 text-[var(--text-dense-label)] transition-colors',
    active
      ? 'border-transparent border-b-2 border-b-[var(--primary)] bg-[var(--control-fill-hover)] font-semibold text-[var(--foreground)]'
      : 'border-transparent bg-[var(--control-fill)] font-medium text-[var(--muted-foreground)] hover:bg-[var(--control-fill-hover)] hover:text-[var(--foreground)]',
  ].join(' ')
}

/** Track-type cards — selected = White; unselected = Gray. No primary fill. */
export function briefingTrackTypeCardClass(selected: boolean): string {
  return [
    'flex items-center gap-2 rounded-[var(--card-radius)] border px-2.5 py-1.5 text-left transition-colors',
    selected
      ? 'border-[var(--foreground)]/20 bg-[var(--card-fill-hover)]'
      : 'border-[var(--card-border)] bg-[var(--card-fill)] opacity-70 hover:opacity-90 hover:bg-[var(--card-fill-hover)]',
  ].join(' ')
}

/** Shared dashed shell for Ready lanes + New Lane entry. Highlight only when selected. */
export function briefingDashedCardClass(selected: boolean, dimmed = false): string {
  if (selected) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--primary)] bg-[var(--primary)]/8 px-3 py-2.5 text-left transition-colors'
  }
  if (dimmed) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--border)]/50 bg-[var(--muted)]/20 px-3 py-2.5 text-left opacity-70 transition-colors hover:opacity-90 hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40'
  }
  return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--border)] bg-transparent px-3 py-2.5 text-left transition-colors hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40 hover:text-[var(--foreground)]'
}

/** Solid lane card — Highlight only for Selected Lane; White otherwise; Gray when dimmed. */
export function briefingSolidCardClass(selected: boolean, dimmed = false): string {
  if (selected) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-[var(--primary)] bg-[var(--primary)]/8 px-3 py-2.5 text-left transition-colors'
  }
  if (dimmed) {
    return 'flex w-full min-w-0 flex-col rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-left opacity-70 transition-colors hover:opacity-90 hover:bg-[var(--card-fill-hover)]'
  }
  return 'flex w-full min-w-0 flex-col rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-left transition-colors hover:bg-[var(--card-fill-hover)]'
}

/** List-row variant of lane selection chrome. */
export function briefingLaneListRowClass(
  selected: boolean,
  opts?: { emptyHint?: boolean; dimmed?: boolean },
): string {
  const emptyHint = opts?.emptyHint === true
  const dimmed = opts?.dimmed === true
  if (selected) {
    return 'flex w-full min-w-0 items-center gap-2 rounded-md border border-[var(--primary)] bg-[var(--primary)]/8 px-2.5 py-1.5 text-left transition-colors'
  }
  if (dimmed) {
    return [
      'flex w-full min-w-0 items-center gap-2 rounded-md border px-2.5 py-1.5 text-left opacity-70 transition-colors hover:opacity-90',
      emptyHint
        ? 'border-dashed border-[var(--border)]/50 bg-transparent hover:bg-[var(--secondary)]/40'
        : 'border-[var(--card-border)] bg-[var(--card-fill)] hover:bg-[var(--card-fill-hover)]',
    ].join(' ')
  }
  return [
    'flex w-full min-w-0 items-center gap-2 rounded-md border px-2.5 py-1.5 text-left transition-colors',
    emptyHint
      ? 'border-dashed border-[var(--border)] bg-transparent hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40'
      : 'border-[var(--card-border)] bg-[var(--card-fill)] hover:bg-[var(--card-fill-hover)]',
  ].join(' ')
}
