import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import type { IbGatewayCutoverEnv } from '@/api/satelliteBusTypes'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'

function reachTagVariant(reach: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

function envOk(env: IbGatewayCutoverEnv): boolean {
  return env.redis_ib_external_name_ok && env.reachability === 'ok' && env.legacy_ib_replicas === 0
}

function envTone(env: IbGatewayCutoverEnv): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  if (envOk(env)) return 'ok'
  if (env.redis_ib_external_name_ok || env.reachability === 'degraded') return 'scheduled'
  return 'missing'
}

function shortNs(ns: string): string {
  return ns.replace(/^bifrost-/, '')
}

function EnvCard({ env }: { env: IbGatewayCutoverEnv }) {
  const ok = envOk(env)
  const fill = ok ? 100 : env.redis_ib_external_name_ok ? 50 : 0
  return (
    <DashCard
      title={shortNs(env.namespace)}
      tag={env.redis_ib_external_name_ok ? 'ext OK' : 'ext MISSING'}
      tagVariant={env.redis_ib_external_name_ok ? 'success' : 'danger'}
      value={String(env.legacy_ib_replicas)}
      unit="legacy rep"
      caption={`reach ${env.reachability}`}
      captionTitle={env.detail}
    >
      <Meter fillPct={fill} toneClass={toneByLevel(envTone(env))} label={env.namespace} />
    </DashCard>
  )
}

export function IbGatewayCutoverStatusPanel({
  embedded = false,
}: {
  embedded?: boolean
} = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const cutover = liveProbe.status?.cutover
  const envs = cutover?.environments ?? []
  const okCount = envs.filter(envOk).length
  const partial = envs.filter(e => !envOk(e) && e.redis_ib_external_name_ok).length
  const bad = envs.length - okCount - partial

  return (
    <OpsSection
      variant={embedded ? 'flat' : 'elevated'}
      title="Trade cutover"
      description="Legacy IB retired · redis-ib ExternalName per Trade NS"
      headerExtra={
        <DenseTag variant={reachTagVariant(cutover?.reachability ?? 'unknown')}>
          {cutover?.legacy_socket_retired === true ? 'LEGACY RETIRED' : 'LEGACY ACTIVE'}
        </DenseTag>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible={!embedded}
      defaultCollapsed={false}
    >
      {envs.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Cutover probe unavailable — ensure platform-api includes cutover in ib-gateway status.
        </p>
      ) : (
        <div className="flex flex-col gap-1.5">
          <div className="flex items-stretch gap-2">
            <ScoreRing
              ready={okCount}
              thin={partial}
              blocked={bad}
              total={Math.max(envs.length, 1)}
              caption="ok"
            />
            <div className="grid min-w-0 flex-1 grid-cols-1 gap-1.5 sm:grid-cols-3">
              {envs.map(env => (
                <EnvCard key={env.namespace} env={env} />
              ))}
            </div>
          </div>

          <OpsSection
            variant="flat"
            title="NS table"
            collapsible
            defaultCollapsed
            bodyPadding="none"
            overflow="visible"
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Namespace</DenseTableHead>
                  <DenseTableHead>Legacy IB replicas</DenseTableHead>
                  <DenseTableHead>redis-ib ExternalName</DenseTableHead>
                  <DenseTableHead>Reach</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {envs.map(env => (
                  <DenseTableRow key={env.namespace}>
                    <DenseTableCell>{env.namespace}</DenseTableCell>
                    <DenseTableCell>{env.legacy_ib_replicas}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={env.redis_ib_external_name_ok ? 'success' : 'danger'}>
                        {env.redis_ib_external_name_ok ? 'OK' : 'MISSING'}
                      </DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={reachTagVariant(env.reachability)}>
                        {env.reachability}
                      </DenseTag>
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        </div>
      )}
    </OpsSection>
  )
}
