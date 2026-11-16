import { describe, expect, it } from 'vitest'
import { CATALOG_VERSION, SCOPE_ROWS, HARDWARE_ROWS } from '@/lib/environments-catalog'
import { tradeEnvTagVariant, tradeEnvSegmentActiveClass } from '@/lib/envVisual'
import { consumeSatelliteApiEnv, consumeSatelliteHealthSection } from '@/lib/task-mode/readinessChipActions'
import { projectPayloadReadinessRows } from '@/lib/control-room/payloadReadiness'
import type { FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import { listFailingMatrixTargets } from '@/lib/control-room/controlRoomOperatePack'
import { buildMissionSnapshot, collectMissionDegradationItems, missionDegradationSummary } from '@/lib/control-room/missionSignals'
import type { MatrixResponse } from '@/api/matrixTypes'

// Golden objects/text/order captured before extraction at b9e9ba19.
describe('environment and observation extraction parity', () => {
 it('preserves full catalog and identity colors', () => {
  expect({ CATALOG_VERSION,SCOPE_ROWS,HARDWARE_ROWS,colors:(['dev','stg','prod'] as const).map(env=>[env,tradeEnvTagVariant(env),tradeEnvSegmentActiveClass(env)]) }).toMatchSnapshot()
 })
 it('preserves navigation consume-and-clear behavior including invalid input', () => {
  const results=[]
  for(const value of ['dev','stg','prod','invalid','']) {sessionStorage.setItem('bifrost.satelliteApi.env',value);results.push([value,consumeSatelliteApiEnv(),sessionStorage.getItem('bifrost.satelliteApi.env')])}
  for(const value of ['probes','runtime','invalid','']) {sessionStorage.setItem('bifrost.satelliteHealth.section',value);results.push([value,consumeSatelliteHealthSection(),sessionStorage.getItem('bifrost.satelliteHealth.section')])}
  expect(results).toMatchSnapshot()
 })
 it('preserves complete payload rows and fallback matching', () => {
  const standards=[{id:'api-monitor',group:'api',signal:'ok',reason:'monitor',label:'Monitor'}, {id:'massive-api-monitor',group:'api',signal:'fail',reason:'exclude',label:'Massive'}, {id:'api-ops',group:'api',signal:'degraded',reason:'ops',label:'Ops'}, {id:'postgres',group:'datastore',signal:'unavailable',reason:'pg missing',label:'PG'}, {id:'redis',group:'datastore',signal:'ok',reason:'redis',label:'Redis'}, {id:'nginx',group:'edge',signal:'fail',reason:'edge fail',label:'Edge'}]
  const fleet={cells:[{key:'satellite:dev',standards},{key:'satellite:stg',standards:[{id:'stg-smoke',signal:'degraded',reason:'smoke'}]},{key:'vendor:span',standards:[{id:'ib-feed',signal:'ok',reason:'shared',label:'IB'}]}]} as unknown as FleetSnapshot
  expect([projectPayloadReadinessRows({cells:[]} as unknown as FleetSnapshot),projectPayloadReadinessRows(fleet)]).toMatchSnapshot()
 })
 it('preserves full observation summaries and mission text', () => {
  const matrices=[{environment:'dev',targets:[{id:'nginx-spa',category:'trade_frontend',reachability:'ok'},{id:'postgres',category:'datastore',reachability:'unknown'},{id:'write',category:'trade_write',auth:'blocked',reachability:'fail'},{id:'ops-capabilities',category:'trade_auth',auth:'skipped',reachability:'unknown'}]},{environment:'prod',targets:[{id:'api-ops',category:'trade_api',reachability:'degraded',detail:'fixture'}]}] as MatrixResponse[]
  const snapshots=[buildMissionSnapshot({matrices:[]}),buildMissionSnapshot({matrices,cluster:{reachability:'ok',nodes_ready:2,nodes_total:3,failing_pods:1} as never,self:{overall:'degraded',probes:[{status:'ok'},{status:'fail'}]} as never,supply:{reachability:'ok',last_deliver_run:{name:'fixture',status:'False',reason:'Failed',pipeline:'fixture'}} as never,stg:{targets:[{reachability:'ok'}]} as never})]
  expect({matrix:matrices.map(summarizeMatrix),failing:listFailingMatrixTargets(matrices),snapshots,degradation:snapshots.map(collectMissionDegradationItems),text:snapshots.map(s=>missionDegradationSummary(collectMissionDegradationItems(s)))}).toMatchSnapshot()
 })
})
