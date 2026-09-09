/**
 * GET /market/ingest/queue-history — what the queue did, over time.
 *
 * The queue dashboard answers "right now", which was the only answer the plugin
 * could give: job_ingest is a work queue whose trim caps finished rows at about
 * an hour, so a rate falling from 1,700 a minute to 666 looked identical to a
 * healthy queue in every single reading. This is the recorded series.
 */

export type QueueHistoryPoint = {
  sample_ts: string;
  kind: string | null;
  pending: number | null;
  running: number | null;
  created_delta: number;
  done_delta: number;
  failed_delta: number;
  oldest_pending_age_sec: number | null;
  p50_sec?: number | null;
  p95_sec: number | null;
};

export type QueueHistory = {
  interval_sec: number;
  hours: number;
  kind: string | null;
  points: QueueHistoryPoint[];
};

export async function fetchQueueHistory(
  hours: number,
  kind?: string,
): Promise<QueueHistory> {
  const q = new URLSearchParams({ hours: String(hours) });
  if (kind) q.set("kind", kind);
  const r = await fetch(
    `/api/v1/plugins/market-data/api/market/ingest/queue-history?${q}`,
  );
  if (!r.ok) throw new Error(`queue history: HTTP ${r.status}`);
  const body = (await r.json()) as { ok: boolean; data: QueueHistory };
  if (!body.ok) throw new Error("queue history: not ok");
  return body.data;
}
