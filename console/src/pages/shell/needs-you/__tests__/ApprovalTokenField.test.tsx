import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { ApprovalTokenField } from '@/pages/shell/needs-you/ApprovalTokenField'

const SECRET = 'w61-saved-token-not-in-dom'
const REPLACEMENT = 'w61-replacement-token-not-in-dom'

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear() {
      mem.clear()
    },
    getItem(key) {
      return mem.has(key) ? (mem.get(key) ?? null) : null
    },
    key(index) {
      return [...mem.keys()][index] ?? null
    },
    removeItem(key) {
      mem.delete(key)
    },
    setItem(key, value) {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

describe('ApprovalTokenField', () => {
  beforeEach(() => {
    installStorage()
  })

  afterEach(() => {
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
  })

  it('starts empty when a token is saved and never puts that token in the HTML', () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, SECRET)
    const { container } = render(<ApprovalTokenField token={SECRET} onSaved={vi.fn()} />)
    const input = screen.getByLabelText('Set approval token') as HTMLInputElement
    expect(input.value).toBe('')
    expect(input.outerHTML).not.toContain(SECRET)
    expect(container.innerHTML).not.toContain(SECRET)
    expect(container.textContent ?? '').not.toContain(SECRET)
    expect(screen.getByText('A token is saved. Type a new one to replace it.')).toBeTruthy()
    expect(screen.getByPlaceholderText('A token is saved. Type a new one to replace it.')).toBeTruthy()
  })

  it('does not erase the saved token when the draft is empty', () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, SECRET)
    const onSaved = vi.fn()
    render(<ApprovalTokenField token={SECRET} onSaved={onSaved} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save token' }))
    fireEvent.change(screen.getByLabelText('Set approval token'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save token' }))
    expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBe(SECRET)
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('clears the input after a replacement is saved, including on the next render', () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, SECRET)
    const onSaved = vi.fn()
    const { container, rerender } = render(<ApprovalTokenField token={SECRET} onSaved={onSaved} />)
    const input = screen.getByLabelText('Set approval token') as HTMLInputElement
    fireEvent.change(input, { target: { value: REPLACEMENT } })
    fireEvent.click(screen.getByRole('button', { name: 'Save token' }))
    expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBe(REPLACEMENT)
    expect(onSaved).toHaveBeenCalledWith(REPLACEMENT)
    expect(input.value).toBe('')
    expect(container.innerHTML).not.toContain(REPLACEMENT)
    rerender(<ApprovalTokenField token={REPLACEMENT} onSaved={onSaved} />)
    expect(input.value).toBe('')
    expect(container.innerHTML).not.toContain(REPLACEMENT)
    expect(container.innerHTML).not.toContain(SECRET)
  })

  it('removes the saved token', () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, SECRET)
    const onSaved = vi.fn()
    render(<ApprovalTokenField token={SECRET} onSaved={onSaved} />)
    fireEvent.click(screen.getByRole('button', { name: 'Remove saved token' }))
    expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull()
    expect(onSaved).toHaveBeenCalledWith('')
  })

  it('has no remove button until a token is saved', () => {
    render(<ApprovalTokenField token="" onSaved={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Remove saved token' })).toBeNull()
    expect(screen.queryByText('A token is saved. Type a new one to replace it.')).toBeNull()
  })
})
