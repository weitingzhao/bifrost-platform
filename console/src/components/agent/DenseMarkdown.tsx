import { Fragment, type ReactNode } from 'react'
import { cn } from '@bifrost/ui'

/** Inline: `code` then **bold** — text nodes only (React escapes). */
function renderInline(text: string): ReactNode[] {
  const nodes: ReactNode[] = []
  const re = /(`[^`]+`|\*\*[^*]+\*\*)/g
  let last = 0
  let m: RegExpExecArray | null
  let key = 0
  while ((m = re.exec(text)) != null) {
    if (m.index > last) {
      nodes.push(<Fragment key={`t${key++}`}>{text.slice(last, m.index)}</Fragment>)
    }
    const token = m[0]
    if (token.startsWith('`')) {
      nodes.push(
        <code key={`c${key++}`} className="dense-md__code">
          {token.slice(1, -1)}
        </code>,
      )
    } else {
      nodes.push(
        <strong key={`b${key++}`} className="dense-md__strong">
          {token.slice(2, -2)}
        </strong>,
      )
    }
    last = m.index + token.length
  }
  if (last < text.length) {
    nodes.push(<Fragment key={`t${key++}`}>{text.slice(last)}</Fragment>)
  }
  return nodes
}

function isTableSep(line: string): boolean {
  const t = line.trim()
  if (!t.includes('|')) return false
  return /^\|?[\s:|-]+\|[\s:|-]*\|?$/.test(t)
}

function splitTableRow(line: string): string[] {
  let s = line.trim()
  if (s.startsWith('|')) s = s.slice(1)
  if (s.endsWith('|')) s = s.slice(0, -1)
  return s.split('|').map(c => c.trim())
}

type Block =
  | { kind: 'h'; level: 2 | 3; text: string }
  | { kind: 'p'; text: string }
  | { kind: 'ul'; items: string[] }
  | { kind: 'table'; headers: string[]; rows: string[][] }
  | { kind: 'pre'; text: string }

function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const blocks: Block[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    const trimmed = line.trim()
    if (trimmed === '') {
      i += 1
      continue
    }
    if (trimmed.startsWith('```')) {
      const body: string[] = []
      i += 1
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        body.push(lines[i])
        i += 1
      }
      if (i < lines.length) i += 1
      blocks.push({ kind: 'pre', text: body.join('\n') })
      continue
    }
    if (trimmed.startsWith('### ')) {
      blocks.push({ kind: 'h', level: 3, text: trimmed.slice(4) })
      i += 1
      continue
    }
    if (trimmed.startsWith('## ')) {
      blocks.push({ kind: 'h', level: 2, text: trimmed.slice(3) })
      i += 1
      continue
    }
    if (trimmed.includes('|') && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      const headers = splitTableRow(trimmed)
      i += 2
      const rows: string[][] = []
      while (i < lines.length && lines[i].trim().includes('|') && !isTableSep(lines[i])) {
        rows.push(splitTableRow(lines[i]))
        i += 1
      }
      blocks.push({ kind: 'table', headers, rows })
      continue
    }
    if (/^[-*]\s+/.test(trimmed) || /^\d+\.\s+/.test(trimmed)) {
      const items: string[] = []
      while (
        i < lines.length &&
        (/^[-*]\s+/.test(lines[i].trim()) || /^\d+\.\s+/.test(lines[i].trim()))
      ) {
        items.push(lines[i].trim().replace(/^([-*]|\d+\.)\s+/, ''))
        i += 1
      }
      blocks.push({ kind: 'ul', items })
      continue
    }
    const para: string[] = [trimmed]
    i += 1
    while (
      i < lines.length &&
      lines[i].trim() !== '' &&
      !lines[i].trim().startsWith('##') &&
      !lines[i].trim().startsWith('```') &&
      !(/^[-*]\s+/.test(lines[i].trim()) || /^\d+\.\s+/.test(lines[i].trim())) &&
      !(lines[i].includes('|') && i + 1 < lines.length && isTableSep(lines[i + 1]))
    ) {
      para.push(lines[i].trim())
      i += 1
    }
    blocks.push({ kind: 'p', text: para.join(' ') })
  }
  return blocks
}

/** Heuristic: treat as markdown when structure markers are present. */
export function looksLikeMarkdown(text: string): boolean {
  const t = text.trim()
  if (t.length < 8) return false
  return (
    /^#{1,3}\s/m.test(t) ||
    /\|.+\|[\r\n]+\|[-:\s|]+\|/m.test(t) ||
    /\*\*[^*]+\*\*/.test(t) ||
    /^[-*]\s+/m.test(t) ||
    /```/.test(t)
  )
}

/**
 * Dense markdown subset for Agent Dock summaries — no HTML passthrough, no new deps.
 * Supports ##/###, tables, lists, fenced code, **bold**, `code`.
 */
export function DenseMarkdown({
  source,
  className,
}: {
  source: string
  className?: string
}) {
  const blocks = parseBlocks(source)
  return (
    <div className={cn('dense-md', className)}>
      {blocks.map((b, idx) => {
        if (b.kind === 'h') {
          const Tag = b.level === 2 ? 'h3' : 'h4'
          return (
            <Tag key={idx} className={`dense-md__h dense-md__h--${b.level}`}>
              {renderInline(b.text)}
            </Tag>
          )
        }
        if (b.kind === 'p') {
          return (
            <p key={idx} className="dense-md__p">
              {renderInline(b.text)}
            </p>
          )
        }
        if (b.kind === 'ul') {
          return (
            <ul key={idx} className="dense-md__ul">
              {b.items.map((item, j) => (
                <li key={j}>{renderInline(item)}</li>
              ))}
            </ul>
          )
        }
        if (b.kind === 'pre') {
          return (
            <pre key={idx} className="dense-md__pre">
              {b.text}
            </pre>
          )
        }
        return (
          <div key={idx} className="dense-md__table-wrap dense-scroll-x">
            <table className="dense-md__table">
              <thead>
                <tr>
                  {b.headers.map((h, j) => (
                    <th key={j}>{renderInline(h)}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {b.rows.map((row, ri) => (
                  <tr key={ri}>
                    {b.headers.map((_, ci) => (
                      <td key={ci}>{renderInline(row[ci] ?? '')}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      })}
    </div>
  )
}
