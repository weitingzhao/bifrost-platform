import { describe, expect, it } from 'vitest'
import type { SocketHealthEnvCell } from '@/lib/satellite/socketHealthSemantics'
import {
  computeEnvDiverges,
  matrixDivergeBucket,
} from '@/lib/satellite/socketHealthSemantics'

function cell(partial: Partial<SocketHealthEnvCell>): SocketHealthEnvCell {
  return {
    reach: 'ok',
    reachLabel: 'ok',
    required: 'required',
    detail: '',
    ...partial,
  }
}

describe('matrixDivergeBucket / computeEnvDiverges', () => {
  it('does not DRIFT when STG is policy-off and other envs are observe', () => {
    const diverges = computeEnvDiverges({
      dev: cell({ reachLabel: 'observe', detail: 'Running · observe' }),
      stg: cell({
        required: 'policy-off',
        reachLabel: 'policy-off',
        detail: 'Daemon scaled to 0 by env policy',
      }),
      prod: cell({ reachLabel: 'observe' }),
      local: cell({ reachLabel: 'observe' }),
    })
    expect(diverges).toBe(false)
    expect(matrixDivergeBucket(cell({ reachLabel: 'observe' }))).toBe('healthy-intentional')
    expect(
      matrixDivergeBucket(
        cell({ required: 'policy-off', reachLabel: 'policy-off' }),
      ),
    ).toBe('healthy-intentional')
  })

  it('still DRIFTs when one env fails while others observe', () => {
    const diverges = computeEnvDiverges({
      dev: cell({ reachLabel: 'observe' }),
      stg: cell({ reach: 'fail', reachLabel: 'fail', detail: 'down' }),
      prod: cell({ reachLabel: 'observe' }),
      local: cell({ reachLabel: 'observe' }),
    })
    expect(diverges).toBe(true)
  })

  it('DRIFTs live ok vs observe (execution posture differs)', () => {
    const diverges = computeEnvDiverges({
      dev: cell({ reachLabel: 'ok' }),
      stg: cell({ reachLabel: 'observe' }),
      prod: cell({ reachLabel: 'observe' }),
      local: cell({ reach: 'unknown', reachLabel: 'unknown', required: 'optional' }),
    })
    expect(diverges).toBe(true)
  })

  it('ignores unprobed local when other envs agree', () => {
    const diverges = computeEnvDiverges({
      dev: cell({ reachLabel: 'observe' }),
      stg: cell({ required: 'policy-off', reachLabel: 'policy-off' }),
      prod: cell({ reachLabel: 'observe' }),
      local: cell({ reach: 'unknown', reachLabel: 'unknown', required: 'optional' }),
    })
    expect(diverges).toBe(false)
  })
})
