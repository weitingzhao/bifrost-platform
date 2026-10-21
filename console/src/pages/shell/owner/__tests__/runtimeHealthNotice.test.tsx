import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import schemaFixture from '@/api/__fixtures__/agentrt-progress-schema.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { RuntimeHealthNotice } from '../RuntimeHealthNotice'

// TD-411. The fixture is GET /agentrt/progress from real rows (store TestProgressSaysTheSchemaAndTheFailingLoops,
// written with CONSOLE_SCHEMA_FIXTURE_WRITE=1): on a database at 0006, and with the facts loop alerted as failing.
const behind = parseRuntimeProgress(schemaFixture.behind)
const failing = parseRuntimeProgress(schemaFixture.failing)
afterEach(cleanup)

describe('what stops the agent runtime controller', () => {
  it('reads the schema and the failing loops the platform gives, and nothing from a platform before them', () => {
    expect(behind.schema).toMatchObject({ state: 'behind', missing: ['0007_gate_tasks', '0008_attempt_prepare_failed'] })
    expect(behind.controller_alerts).toEqual([])
    expect(failing.schema.state).toBe('current')
    expect(failing.controller_alerts).toHaveLength(1)
    expect(failing.controller_alerts[0]).toMatchObject({ loop: 'agentrt-facts-rules' })
    expect(parseRuntimeProgress({}).schema.state).toBe('unread')
    expect(parseRuntimeProgress({ schema: { state: 'odd' } }).schema.state).toBe('unknown')
  })

  it('says the database misses the migrations, and what to run', () => {
    render(<RuntimeHealthNotice progress={behind} now={Date.now()} />)
    expect(screen.getByRole('alert', { name: 'Agent runtime health' })).toHaveTextContent(
      "The agent runtime's database is missing migrations 0007_gate_tasks, 0008_attempt_prepare_failed: parts of the controller have stopped. Run the agentrt-migrate Job for this environment.")
  })

  it('says which loop fails since when, with its error', () => {
    const since = Date.parse(failing.controller_alerts[0].since)
    render(<RuntimeHealthNotice progress={failing} now={since + 12 * 60_000} />)
    const alert = screen.getByRole('alert', { name: 'Agent runtime health' })
    expect(alert).toHaveTextContent("The controller's agentrt-facts-rules has failed the same way since 12 min ago.")
    expect(alert).toHaveTextContent('violates check constraint "task_check2"')
  })

  it('says nothing when all is well, when the runtime is not read, or from a platform before TD-411', () => {
    const { container } = render(<>
      <RuntimeHealthNotice progress={{ ...failing, controller_alerts: [] }} now={Date.now()} />
      <RuntimeHealthNotice progress={undefined} now={Date.now()} />
      <RuntimeHealthNotice progress={parseRuntimeProgress({})} now={Date.now()} />
    </>)
    expect(container).toBeEmptyDOMElement()
  })

  it('says an edited, an ahead and an unreadable schema', () => {
    const with_ = (schema: object) => parseRuntimeProgress({ ...schemaFixture.failing, controller_alerts: [], schema })
    render(<>
      <RuntimeHealthNotice progress={with_({ state: 'edited', edited: ['0003_grants'] })} now={Date.now()} />
      <RuntimeHealthNotice progress={with_({ state: 'ahead', unknown: [9] })} now={Date.now()} />
      <RuntimeHealthNotice progress={with_({ state: 'unknown', reason: 'permission denied' })} now={Date.now()} />
    </>)
    const [edited, ahead, unknown] = screen.getAllByRole('alert')
    expect(edited).toHaveTextContent('applied 0003_grants with other contents than this platform')
    expect(ahead).toHaveTextContent('has migration 9 that this platform does not carry')
    expect(unknown).toHaveTextContent('could not be read: permission denied')
  })
})
