// 2026-11-24 箭10: every view's frame says "N laws · M actions" waiting. The shell publishes the numbers once; a frame
// only reads them (a hosted view mounts no readers of its own), and outside the shell (a preview page) it reads
// nothing and the frame draws nothing.
import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { publishFrameWaiting, useFrameWaiting } from '@/lib/shell/frameWaiting'

describe('frame waiting', () => {
  afterEach(() => publishFrameWaiting(undefined))
  it('reads nothing outside the shell', () => {
    expect(renderHook(() => useFrameWaiting()).result.current).toBeUndefined()
  })
  it('reads what the shell published, and follows a new count', () => {
    const { result } = renderHook(() => useFrameWaiting())
    act(() => publishFrameWaiting({ laws: 2, actions: 5, signoffs: 6, href: '#command?section=waiting' }))
    expect(result.current).toEqual({ laws: 2, actions: 5, signoffs: 6, href: '#command?section=waiting' })
    act(() => publishFrameWaiting({ laws: null, actions: 5, signoffs: 6, href: '#command?section=waiting', why: 'questions not read' }))
    expect(result.current?.laws).toBeNull()
  })
})
