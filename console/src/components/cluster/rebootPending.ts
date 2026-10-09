export type RebootSample = {
  labels?: Record<string, string>
  value: number
}

/** Unix mtime seconds → UTC calendar date. Null when the sample is not a real mtime. */
export function formatRebootSince(seconds: number): string | null {
  if (!Number.isFinite(seconds) || seconds <= 0) return null
  const date = new Date(seconds * 1000)
  if (Number.isNaN(date.getTime())) return null
  return date.toISOString().slice(0, 10)
}

/**
 * Match Prometheus `node` labels to a cluster node.
 * Returns null when the node does not need a reboot, or when the since-seconds sample is missing.
 */
export function rebootPendingLabel(
  nodeName: string,
  required: RebootSample[] | null | undefined,
  since: RebootSample[] | null | undefined,
): string | null {
  if (required == null || since == null) return null
  const needsReboot = required.some(point => point.labels?.node === nodeName && point.value === 1)
  if (!needsReboot) return null
  const sincePoint = since.find(point => point.labels?.node === nodeName)
  if (sincePoint == null) return null
  const date = formatRebootSince(sincePoint.value)
  if (date == null) return null
  return `Reboot pending (since ${date})`
}
