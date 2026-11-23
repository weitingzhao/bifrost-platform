import { describe, it, expect } from 'vitest'
import { readOperations } from '../operations'
const at = '2026-11-22T12:00:00Z'
const light = {value:'good',why:'1 workload ready'}
describe('existing operations contracts', () => {
  it('keeps unknown domains, zero counts, partial history and source verdicts distinct', async () => {
    const result = await readOperations(async path => {
      if (path.endsWith('/catalog/status')) return {at,domains:[{id:'compute',alive:light,interfaces:{value:'unknown',why:'No traffic samples'},boundary:light,fresh:light}]}
      if (path.endsWith('/pipelines')) return {generated_at:at,pipelines:[{name:'bifrost-ci-platform'},{name:'bifrost-deliver-platform'}]}
      if (path.endsWith('/runs')) return {generated_at:at,runs:[{name:'old',status:'False',start_time:'2026-11-20T12:00:00Z'},{name:'now',status:'True',start_time:at,reason:'Succeeded'}]}
      return {generated_at:at,overall:'healthy',detail:'Source verdict',lanes:[{id:'batch',label:'Batch',verdict:'healthy',detail:'0 waiting'}]}
    })
    const text=JSON.stringify(result.panels)
    expect(text).toContain('No traffic samples')
    expect(text).toContain('Retained records only')
    expect(text).toContain('"label":"Observed / 24h","value":1')
    expect(text).toContain('Succeeded')
    expect(text).toContain('0 waiting')
    expect(result.panels.ci).toHaveLength(1)
    expect(result.evidence['domain:compute'][0].evaluatedAt).toBe(at)
    // Re-reading an old worker snapshot must not make its measurement fresh.
    expect(result.evidence['domain:compute'][0].expiresAt).toBe('2026-11-22T12:01:30.000Z')
  })
  it('preserves an explicit denied source while independent CI and data finish', async () => {
    const result = await readOperations(async path => {
      if (path.endsWith('/catalog/status')) throw new Error('viewer token required')
      if (path.endsWith('/pipelines')) return {pipelines:[],generated_at:at}
      return {generated_at:at,overall:'unknown',detail:'Source did not measure',lanes:[]}
    })
    expect(result.panels.domains).toHaveLength(14)
    expect(result.evidence['domain:foundation'][0].outcome).toBe('error')
    expect(JSON.stringify(result.panels.data)).toContain('Source did not measure')
    expect(JSON.stringify(result.panels.ci)).toContain('No CI pipelines returned')
  })
})
