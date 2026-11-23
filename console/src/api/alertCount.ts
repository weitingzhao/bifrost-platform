/**
 * How many alerts are firing, counted the one way every view counts them (the metrics line's rule,
 * 2026-11-23, m_01M84K76…; its recording rules bifrost_alerts:firing{severity} and
 * bifrost_alerts:firing_total carry the same rule once they take effect):
 *   counted      alertstate firing (pending has not held its `for` yet)
 *   not counted  severity none (the alert chain's own heartbeat: Watchdog, InfoInhibitor), drill="true"
 *   info         counted, a grade of its own; a severity the rules do not name reads info
 *   one series   one alert: the same rule firing in two namespaces counts 2
 *   silenced     still counted (a silence does not make the alert go away)
 *   worst        critical > warning > info; none when nothing fires
 * Read from GET /api/v1/telemetry/alerts. The City's alerts corner and the Console count with this, so one
 * minute reads one number on every view (2026-11-23: the City said 6 firing, the Console 5 warning).
 */
export type AlertGrade = 'critical' | 'warning' | 'info'
export const ALERT_GRADES: readonly AlertGrade[] = ['critical', 'warning', 'info']

type Alert = { labels: Record<string, string>; state: string }

/** Whether the rule counts this alert. */
export const counted = (a: Alert): boolean =>
  a.state === 'firing' && a.labels.severity !== 'none' && a.labels.drill !== 'true'

/** The grade an alert counts under. */
export const gradeOf = (a: Alert): AlertGrade =>
  (ALERT_GRADES as readonly string[]).includes(a.labels.severity) ? a.labels.severity as AlertGrade : 'info'

export type AlertCount = { count: number; worst: AlertGrade | null; by_severity: Record<AlertGrade, number> }

export function alertCount(alerts: readonly Alert[]): AlertCount {
  const by_severity: Record<AlertGrade, number> = { critical: 0, warning: 0, info: 0 }
  for (const a of alerts) if (counted(a)) by_severity[gradeOf(a)]++
  const count = by_severity.critical + by_severity.warning + by_severity.info
  return { count, worst: ALERT_GRADES.find(g => by_severity[g] > 0) ?? null, by_severity }
}
