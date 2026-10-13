import { describe, expect, it } from 'vitest'
import { containsSignInData, signInSafeError } from '@/lib/signInError'

describe('reflected sign-in errors', () => {
  const token = 'abcdefgh/ijklmnop+qrstuvwx'
  const reflected = [token, encodeURIComponent(token), ...Array.from({ length: token.length - 7 }, (_, i) => token.slice(i, i + 8))]
  it.each(reflected)('hides the whole sentence when it contains %s', value => {
    const safe = signInSafeError(`server: ${value} was refused`, token, 400)
    expect(safe).toBe('The server returned an error (HTTP 400). The details are hidden because they contained sign-in data.')
    expect(safe).not.toContain(value)
  })
  it('hides JSON-escaped and percent-encoded fragments, including lowercase escapes', () => {
    for (const value of [JSON.stringify('a"bcdefgh'), '%2fijklmn%6f', encodeURIComponent(token).toLowerCase()]) {
      expect(containsSignInData(value, value.startsWith('"') ? 'a"bcdefgh-more' : token)).toBe(true)
    }
  })
  it('preserves useful error details without reflected sign-in data', () => {
    expect(signInSafeError('The request was already closed.', token, 409)).toBe('The request was already closed.')
    expect(signInSafeError('No sign-in is present.', '', 401)).toBe('No sign-in is present.')
  })
  it('still hides a short token in full', () => {
    expect(signInSafeError('reflected tiny', 'tiny', 400)).toContain('details are hidden')
  })
})
