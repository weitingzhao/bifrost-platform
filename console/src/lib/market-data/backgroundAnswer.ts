/**
 * Reading an answer that is computed behind a cache.
 *
 * Three plugin endpoints take minutes against the real tables — the three-axis
 * coverage read, the inventory, and the readiness summary — and the API gateway
 * gives up at 60 seconds. Rather than hold the request, they serve the last
 * answer, start a recompute behind it, and say how old it is. A panel reading
 * one of them has to tell three states apart: no answer yet, a stale answer
 * with a fresher one coming, and a current answer.
 *
 * "Still counting" is not "counted, and it is zero" — the Overview tab marked
 * all six analytics products blocked because it could not tell the difference.
 */

export type BackgroundAnswer = {
  computing?: boolean;
  age_sec?: number | null;
  computed_ms?: number;
};

export function isComputing(answer: unknown): boolean {
  return (answer as BackgroundAnswer | null | undefined)?.computing === true;
}

export function ageSec(answer: unknown): number | null {
  const v = (answer as BackgroundAnswer | null | undefined)?.age_sec;
  return typeof v === "number" ? v : null;
}

/** "4m ago" / "just now", or null when the answer carries no age. */
export function ageLabel(answer: unknown): string | null {
  const sec = ageSec(answer);
  if (sec == null) return null;
  if (sec < 90) return "just now";
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  return `${Math.round(min / 60)}h ago`;
}

/** Poll faster while a recompute is running, then settle back to `idleMs`. */
export function pollWhileComputing(idleMs: number, busyMs = 15_000) {
  return (query: { state: { data?: unknown } }): number =>
    isComputing(query.state.data) ? busyMs : idleMs;
}
