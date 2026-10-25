import { Bar, Card, Stat } from './OwnerParts'
import { plural, type Tone } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

function nodeTone(status: string, mode: string | undefined): Tone {
  if (mode === 'standby') return 'muted'
  return status === 'Ready' ? 'ok' : 'fail'
}

/** System: platform alerts and cluster nodes. Delivery belongs to Work. */
export function OwnerSystem({ data }: { data: OwnerConsoleData }) {
  const { alerts, nodes, nodeCount } = data
  const yoursCritical = alerts?.yours.filter(r => r.tone === 'fail').length ?? 0
  return <section aria-label="System" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">System</h2>
    <div className="grid gap-4 @3xl:grid-cols-2">
      <Card label="Health" title="Health" more={{ href: '#status', text: 'Status' }}>
        {data.alertsError && <p role="alert" className="m-0 text-sm">Alerts could not be read: {data.alertsError}</p>}
        <div className="grid grid-cols-2 gap-2">
          <Stat value={alerts?.critical ?? (data.alertsError ? '?' : '…')} label={yoursCritical > 0 && yoursCritical === alerts?.critical ? 'critical · yours' : 'critical'} tone={alerts && alerts.critical > 0 ? 'fail' : undefined} />
          <Stat value={alerts?.warning ?? (data.alertsError ? '?' : '…')} label="warnings firing" tone={alerts && alerts.warning > 0 ? 'warn' : undefined} />
        </div>
        {alerts && <p className="m-0 text-sm text-muted-foreground">Plus {plural(alerts.noise, 'alert')} from planned standby, info and the watchdog{alerts.merged > 0 ? `, and ${alerts.merged} told with another` : ''}.</p>}
      </Card>
    </div>
    <Card label="Nodes" title={nodeCount ? `Nodes · ${nodeCount.ready} of ${nodeCount.total} ready${nodeCount.standby ? `, ${nodeCount.standby} on standby` : ''}` : 'Nodes'} more={{ href: '#infrastructure', text: 'Infrastructure' }}>
      {data.nodesError && <p role="alert" className="m-0 text-sm">Nodes could not be read: {data.nodesError}</p>}
      {!data.nodesError && nodes.length === 0 && <p className="m-0 text-sm text-muted-foreground">No nodes reported yet.</p>}
      <ul className="m-0 grid list-none grid-cols-2 gap-2 p-0 @xl:grid-cols-3 @5xl:grid-cols-6">{nodes.map(node => {
        const tone = nodeTone(node.status, node.elastic_mode)
        const standby = node.elastic_mode === 'standby'
        return <li key={node.name} className="flex min-w-0 flex-col gap-1.5 rounded-[var(--control-radius)] border-t-4 bg-[var(--control-fill)] p-2.5" style={{ borderTopColor: `var(${tone === 'ok' ? '--color-lamp-green' : tone === 'fail' ? '--color-lamp-red' : '--color-lamp-gray'})` }}>
          <div className="flex justify-between gap-1"><span className="truncate font-mono text-sm font-semibold" title={node.name}>{node.name.replace(/^ubt-/, '')}</span><span className="text-xs font-semibold">{standby ? 'Standby' : node.status}</span></div>
          <span className="truncate text-xs text-muted-foreground" title={node.roles}>{node.roles || node.workload_label || '—'}</span>
          {!standby && <>
            <div className="flex justify-between text-xs"><span>CPU</span><span className="font-mono">{node.cpu_usage_percent != null ? `${node.cpu_usage_percent.toFixed(1)}%` : '—'}</span></div>
            <Bar percent={node.cpu_usage_percent ?? 0} tone="info" />
            <div className="flex justify-between text-xs"><span>Memory</span><span className="font-mono">{node.memory_usage_percent != null ? `${node.memory_usage_percent.toFixed(1)}%` : '—'}</span></div>
            <Bar percent={node.memory_usage_percent ?? 0} tone="info" />
          </>}
        </li>
      })}</ul>
    </Card>
  </section>
}
