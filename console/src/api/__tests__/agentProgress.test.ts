import { describe, expect, it } from 'vitest'
import { actionLink } from '@/api/agentProgress'

describe('actionLink', () => {
  it('names the destination of a trusted or hash link and hides the rest', () => {
    expect(actionLink({ text: 'Open Needs you', link: 'https://ops.bifrost.lan/needs', link_text: 'Open Needs you' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'Open Needs you (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'http://OPS.BIFROST.LAN/x' }).label).toBe('Open (ops.bifrost.lan)')
    expect(actionLink({ text: 'Jump', link: '#progress' })).toEqual({
      kind: 'anchor',
      href: '#progress',
      label: 'Jump (this console)',
      external: false,
    })
    expect(actionLink({ text: 'Read the note', link: 'https://evil.example/phish', link_text: 'Approve the release' })).toEqual({
      kind: 'blocked',
      text: 'Read the note',
      host: 'evil.example',
    })
    expect(actionLink({ text: 'Open', link: 'https://ops.bifrost.lan:443/needs' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'Open (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'http://ops.bifrost.lan:80/needs' })).toEqual({
      kind: 'anchor',
      href: 'http://ops.bifrost.lan/needs',
      label: 'Open (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Open', link: 'https://ops.bifrost.lan:8443/' })).toEqual({
      kind: 'blocked',
      text: 'Open',
      host: 'ops.bifrost.lan:8443',
    })
  })

  it('cleans and bounds the fallback label, and does not build the destination from it', () => {
    const controls = '\u0000\u0007\u061C\u200E\u200F\u202E\uFEFF'
    const link = `#${'a'.repeat(4998)}\u200F`
    expect(link).toHaveLength(5000)
    const bounded = actionLink({ text: controls, link_text: controls, link })
    expect(bounded).toMatchObject({ kind: 'anchor', href: link, external: false })
    if (bounded.kind !== 'anchor') return
    expect(bounded.href).toBe(link)
    expect(bounded.label.includes('\u200F')).toBe(false)
    expect(bounded.label.includes('\u0000')).toBe(false)
    expect(bounded.label.length).toBeLessThanOrEqual(200)
    expect(bounded.label).not.toBe('Open link')
    expect(bounded.href).not.toBe(bounded.label)

    const hidden = `#${'\u200F'.repeat(4999)}`
    expect(hidden).toHaveLength(5000)
    expect(actionLink({ text: controls, link_text: controls, link: hidden })).toEqual({
      kind: 'anchor',
      href: hidden,
      label: 'Open link',
      external: false,
    })

    const trusted = `https://ops.bifrost.lan/${'b'.repeat(5000)}`
    const longUrl = actionLink({ link: trusted })
    expect(longUrl).toMatchObject({ kind: 'anchor', external: true })
    if (longUrl.kind !== 'anchor') return
    expect(longUrl.href).toBe(trusted)
    expect(longUrl.href).not.toBe(longUrl.label)
    expect(longUrl.label.length).toBeLessThanOrEqual(200)
    expect(longUrl.label).not.toBe('Open link')

    expect(actionLink({ link: 'https://ops.bifrost.lan/needs' })).toEqual({
      kind: 'anchor',
      href: 'https://ops.bifrost.lan/needs',
      label: 'https://ops.bifrost.lan/needs (ops.bifrost.lan)',
      external: true,
    })
    expect(actionLink({ text: 'Read the note', link_text: '\u200F', link: 'https://ops.bifrost.lan/needs' }).label).toBe(
      'Read the note (ops.bifrost.lan)',
    )
    expect(actionLink({ text: 'Body', link_text: 'Link name', link: '#progress' })).toEqual({
      kind: 'anchor',
      href: '#progress',
      label: 'Link name (this console)',
      external: false,
    })
    expect(actionLink({ link: '#progress' }).label).toBe('#progress (this console)')
    expect(actionLink({ link: '#' })).toEqual({
      kind: 'anchor',
      href: '#',
      label: 'Open link',
      external: false,
    })
  })
})
