/**
 * Parses json tags from Go `type Approval struct { ... }` for contract tests.
 * Throws when the struct is missing or too few tags were extracted (parser miss).
 */

/** Conservative floor — real Approval has ~15 fields; avoids tying tests to exact count. */
export const APPROVAL_JSON_TAG_COUNT_LOWER_BOUND = 9

const APPROVAL_STRUCT_HEAD = /type Approval struct \{/

function approvalStructBody(source: string): string | null {
  const head = APPROVAL_STRUCT_HEAD.exec(source)
  if (!head) return null
  let i = head.index + head[0].length
  let depth = 1
  const bodyStart = i
  while (i < source.length && depth > 0) {
    const ch = source[i]
    if (ch === '{') depth += 1
    else if (ch === '}') depth -= 1
    i += 1
  }
  if (depth !== 0) return null
  return source.slice(bodyStart, i - 1)
}

export function approvalJsonTagsFromGo(source: string): string[] {
  const block = approvalStructBody(source)
  const tags: string[] = []
  if (block) {
    for (const line of block.split('\n')) {
      const m = line.match(/json:"([^"]+)"/)
      if (m) tags.push(m[1].replace(/,omitempty$/, ''))
    }
  }
  if (tags.length < APPROVAL_JSON_TAG_COUNT_LOWER_BOUND) {
    throw new Error(
      `Go Approval struct not parsed (found ${tags.length} json tag(s), need at least ${APPROVAL_JSON_TAG_COUNT_LOWER_BOUND}). ` +
        'Rename or reshape of type Approval in types.go may have broken the contract test parser — not a missing fixture field.',
    )
  }
  return tags
}
