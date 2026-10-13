/** A long reflected slice is sign-in data too; hide the whole sentence. */
export function containsSignInData(text: string, token: string): boolean {
  if (token === '') return false
  // Decode each escape separately so a truncated escape elsewhere does not
  // prevent detecting an encoded slice. Token strings are compared literally.
  const decoded = text.replace(/(?:%[0-9a-f]{2})+/gi, part => {
    try { return decodeURIComponent(part) } catch { return part }
  })
  for (const candidate of [text, decoded]) {
    if (candidate.includes(token) || candidate.includes(JSON.stringify(token).slice(1, -1)) || candidate.toLowerCase().includes(encodeURIComponent(token).toLowerCase())) return true
    for (let start = 0; start + 8 <= token.length; start++) {
      const slice = token.slice(start, start + 8)
      if (candidate.includes(slice) || candidate.includes(JSON.stringify(slice).slice(1, -1)) || candidate.toLowerCase().includes(encodeURIComponent(slice).toLowerCase())) return true
    }
  }
  return false
}

export function signInSafeError(text: string, token: string, status?: number): string {
  if (!containsSignInData(text, token)) return text
  const http = status == null ? '' : ` (HTTP ${status})`
  return `The server returned an error${http}. The details are hidden because they contained sign-in data.`
}
