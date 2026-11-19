import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Menu, Pager, pageOf, Tabs, useConfirm } from '../parts'

// 2026-11-19, the Owner: the frame gives every panel the same parts and fixes where they go, like Bootstrap.
afterEach(cleanup)

describe('pageOf', () => {
  it('pages count from 1 and say what they show', () => {
    const rows = Array.from({ length: 23 }, (_, i) => i)
    expect(pageOf(rows, 1, 10)).toMatchObject({ rows: rows.slice(0, 10), page: 1, pages: 3, from: 1, to: 10, total: 23 })
    expect(pageOf(rows, 3, 10)).toMatchObject({ rows: [20, 21, 22], from: 21, to: 23 })
  })
  it('a page past the end or before the start is held to the ends', () => {
    expect(pageOf([1, 2, 3], 9, 2)).toMatchObject({ page: 2, rows: [3] })
    expect(pageOf([1, 2, 3], 0, 2)).toMatchObject({ page: 1, rows: [1, 2] })
  })
  it('an empty list is one empty page', () => {
    expect(pageOf([], 1, 5)).toMatchObject({ rows: [], page: 1, pages: 1, from: 0, to: 0, total: 0 })
  })
})

describe('Tabs', () => {
  const tabs = [{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }]
  it('move with the arrow keys', () => {
    const on = vi.fn()
    render(<Tabs tabs={tabs} value="a" onChange={on} />)
    fireEvent.keyDown(screen.getByRole('tab', { name: 'A' }), { key: 'ArrowRight' })
    expect(on).toHaveBeenCalledWith('b')
  })
  it('in a card become a drop-down', () => {
    render(<Tabs tabs={tabs} value="a" onChange={() => {}} compact />)
    expect(screen.queryByRole('tablist')).toBeNull()
    expect(screen.getByRole('combobox')).toHaveValue('a')
  })
})

describe('Pager', () => {
  it('cannot go before the first page or past the last', () => {
    render(<Pager page={1} pages={1} from={1} to={3} total={3} onPage={() => {}} />)
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
    expect(screen.getByText('1–3 of 3')).toBeInTheDocument()
  })
})

describe('Menu and confirm', () => {
  it('the ⋯ menu runs an item and closes', () => {
    const on = vi.fn()
    render(<Menu items={[{ label: 'Copy', onClick: on }]} />)
    fireEvent.click(screen.getByRole('button', { name: 'More' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Copy' }))
    expect(on).toHaveBeenCalled()
    expect(screen.queryByRole('menu')).toBeNull()
  })
  it('a reset asks first, with the action at the right and Cancel before it', () => {
    const done = vi.fn()
    function Probe() {
      const [ask, node] = useConfirm()
      return <><button onClick={() => ask({ title: 'Reset?', body: 'b', action: 'Reset', danger: true, onConfirm: done })}>go</button>{node}</>
    }
    render(<Probe />)
    fireEvent.click(screen.getByRole('button', { name: 'go' }))
    const buttons = screen.getAllByRole('button').filter(b => ['Cancel', 'Reset'].includes(b.textContent ?? ''))
    expect(buttons.map(b => b.textContent)).toEqual(['Cancel', 'Reset'])
    expect(done).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }))
    expect(done).toHaveBeenCalled()
  })
})
