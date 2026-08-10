/**
 * Repair streamed / smashed GFM tables where row boundaries collapsed to "||".
 * Empty in-row cells usually look like `| |` (pipe-space-pipe); smashed rows use `||`.
 */
export function normalizeMarkdownTables(src: string): string {
  let text = src.replace(/\r\n/g, '\n')

  // Header cell glued to separator without newline:
  // "| 详情 |-------|-------|" → "| 详情 |\n|-------|-------|"
  // Skip when the left side is itself separator-only ("|---|---|").
  text = text.replace(
    /(\|[^|\n]*)\|([ \t]*:?-{3,}[ \t]*\|(?:[ \t]*:?-{3,}[ \t]*\|)*)/g,
    (full, left: string, sep: string) => {
      const cell = left.replace(/^\|/, '').trim()
      if (cell === '' || /^:?-{3,}$/.test(cell) || /^[\s|:\\-]+$/.test(cell)) {
        return full
      }
      return `${left}|\n|${sep}`
    },
  )

  // Header glued to separator via "||": "| A | B ||---|---|"
  text = text.replace(
    /(\|)[ \t]*(\|(?:[\t ]*:?-{3,}[\t ]*\|)+)/g,
    '$1\n$2',
  )

  // Separator glued to next row: "|---|---||| Primary" or "|---|| | Primary" or "|---| | Primary"
  text = text.replace(
    /(\|(?:[\t ]*:?-{3,}[\t ]*\|)+)[ \t]*\|+[ \t]*/g,
    '$1\n| ',
  )

  // Content rows glued with zero-space "||" (keep "| |" empty cells intact)
  let prev = ''
  for (let i = 0; i < 32 && prev !== text; i += 1) {
    prev = text
    text = text.replace(/\|\|(?=[^\n])/g, '|\n|')
  }

  // Cleanup: "|\n| |" → "|\n|" when an extra empty lead-in was introduced
  text = text.replace(/\|\n\|[ \t]+\|/g, '|\n|')

  return text
}

/** Heuristic: treat as markdown when structure markers are present. */
export function looksLikeMarkdown(text: string): boolean {
  const t = normalizeMarkdownTables(text).trim()
  if (t.length < 4) return false
  return (
    /^#{1,3}\s/m.test(t) ||
    /\|.+\|[\r\n]+\|[-:\s|]+\|/m.test(t) ||
    /\*\*[^*]+\*\*/.test(t) ||
    /^[-*]\s+/m.test(t) ||
    /```/.test(t)
  )
}
