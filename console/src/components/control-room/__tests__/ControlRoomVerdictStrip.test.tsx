import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ControlRoomVerdictStrip } from '@/components/control-room/ControlRoomVerdictStrip'

describe('ControlRoomVerdictStrip stale sources (TD-249)', () => {
  it('names the stale probes beside freshness', () => {
    render(
      <ControlRoomVerdictStrip
        missionSignal="unknown"
        primaryCause="matrix probe failing"
        dataUpdatedAt={Date.now() - 5_000}
        staleSources={['matrix', 'runner']}
        bays={[]}
      />,
    )
    expect(screen.getByText('stale: matrix, runner')).toBeTruthy()
  })

  it('says nothing about staleness while probing', () => {
    render(
      <ControlRoomVerdictStrip
        missionSignal="unknown"
        primaryCause=""
        dataUpdatedAt={0}
        staleSources={['matrix']}
        bays={[]}
        isLoading
      />,
    )
    expect(screen.queryByText(/stale:/)).toBeNull()
  })
})
