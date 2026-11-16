import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import { RunHistoryPanel } from '../RunHistoryPanel'
import { SkillObservationCells } from '../SkillObservationCells'
const skill: PatrolSkill = { id: 'read', name: 'Read checks', description: 'Observe only', schedule: '*/5 * * * *', prompt_template: '', mcp_tools: [], trust_level: 'L0', scope: '', timeout_seconds: 10, enabled: true }
const now = Date.parse('2026-11-16T12:00:30Z')
const runs: PatrolRun[] = [
 { id: 'a', skill_id: 'read', skill_name: 'Read checks', trigger: 'cron', started_at: '2026-11-16T12:00:00Z', result: 'running', evidence: 'first log' },
 { id: 'b', skill_id: 'read', skill_name: 'Read checks', trigger: 'manual', started_at: '2026-11-16T11:00:00Z', result: 'success', duration_ms: 1250, evidence: 'second log' },
]
afterEach(cleanup)
function History({ history = runs, loading = false }: { history?: PatrolRun[]; loading?: boolean }) {
 const [id, setId] = useState<string | null>(null)
 const [lang, setLang] = useState<'en' | 'zh'>('en')
 return <RunHistoryPanel skill={skill} history={history} isLoading={loading} logRunId={id} setLogRunId={setId} lang={lang} setOutputLanguage={setLang} now={now} />
}
describe('complete read-only patrol panels', () => {
 it('retains loading and empty-history states', () => {
  const view = render(<History loading />)
  expect(screen.getByText('Loading runs…')).toBeTruthy()
  view.rerender(<History history={[]} />)
  expect(screen.getByText('No runs recorded yet.')).toBeTruthy()
 })
 it('retains elapsed time, selected logs, deselection fallback and language controls', () => {
  render(<History />)
  expect(screen.getByText('30.0s')).toBeTruthy()
  expect(screen.getByText('1.3s')).toBeTruthy()
  expect(screen.getByText('Dispatch log · cron · running')).toBeTruthy()
  fireEvent.click(screen.getByTitle('second log'))
  expect(screen.getByText('Dispatch log · manual · success')).toBeTruthy()
  fireEvent.click(screen.getByTitle('second log'))
  expect(screen.getByText('Dispatch log · cron · running')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '中文' }))
  expect(screen.getByRole('button', { name: '中文' }).getAttribute('aria-pressed')).toBe('true')
 })
 it('renders exactly seven observation cells and forwards only history navigation', () => {
  const toggle = vi.fn()
  const { container } = render(<table><tbody><tr><SkillObservationCells skill={skill} expanded skillRunning now={now} toggleSkill={toggle} /></tr></tbody></table>)
  expect(container.querySelectorAll('td')).toHaveLength(7)
  fireEvent.click(screen.getByRole('button', { name: 'Read checks' }))
  expect(toggle).toHaveBeenCalledWith('read')
  expect(screen.queryByRole('button', { name: 'Run' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Disable' })).toBeNull()
 })
})
