import { it, expect } from 'vitest'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname } from 'node:path'
import { render, fireEvent, screen, cleanup } from '@testing-library/react'
// Read-only integration probe; points to the communication owner's model via COMMUNICATION_MODEL.
import { exchangesOf, evidenceThrough, STAGES, STAGE_LABEL } from '@bifrost/ui/communication'
import { ObservationSurface, type ObservationSnapshot } from '@bifrost/ui/city/observation'

// 2026-11-22 (ISSUE-61): shared communication semantics survive layer switches, duplicates and ACK-before-send.
it('consumes the existing communication contract without rebuilding its state machine', () => {
  const at = '2026-11-22T12:00:00Z'
  const sent = { id: 's1', communication: 'm1', channel: 'message', stage: 'sent', from: 'agent-a', to: 'agent-b', evidence: { id: 's1', source: 'fixture:ledger', at } }
  const ack = { ...sent, id: 'a1', stage: 'acknowledged', evidence: { id: 'a1', source: 'fixture:ledger', at } }
  const exchanges = evidenceThrough(exchangesOf([ack, sent, ack]), at)
  expect(exchanges).toHaveLength(1)
  expect(exchanges[0].stages.acknowledged).toHaveLength(1)
  expect(exchanges[0].stages.completed).toBeUndefined()
  const snapshot: ObservationSnapshot = { version: 1, id: 'comm-fixture', capturedAt: at, provenance: 'fixture', evidence: {}, content: { tree: { id: 'town', label: 'Fixture', kind: 'town', shape: '8x8', question: 'Contract probe', dashboard: null, children: [] } }, layerPanels: { communication: [{ id: 'm1', label: 'm1 · message', blocks: [{ kind: 'facts', rows: STAGES.map((stage: string) => ({ label: STAGE_LABEL[stage], value: exchanges[0].stages[stage]?.map((e: { id: string; source: string; at: string }) => `${e.id} · ${e.source} · ${e.at}`).join(', ') || 'No evidence' })) }] }] } }
  if (process.env.OBSERVATION_PROBE_OUTPUT) {
    mkdirSync(dirname(process.env.OBSERVATION_PROBE_OUTPUT), { recursive: true })
    writeFileSync(process.env.OBSERVATION_PROBE_OUTPUT, JSON.stringify(snapshot, null, 2) + '\n')
  }
  render(<ObservationSurface snapshot={snapshot} layers={[{ id: 'communication', label: 'Communication', present: s => s.layerPanels?.communication ?? [] }]} />)
  fireEvent.click(screen.getByRole('checkbox', { name: 'Communication' }))
  expect(screen.getAllByText(/a1 · fixture:ledger/)).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Status field' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'Communication' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'Communication' }))
  expect(screen.getAllByText(/a1 · fixture:ledger/)).toHaveLength(1)
  expect(screen.getByText('Completed')).toBeTruthy()
  expect(exchanges[0].stages.completed).toBeUndefined()
  cleanup()
})
