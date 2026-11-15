import { OpsSection } from '@/components/layout/OpsSection'

// Source: CI lane, benchmark/time-ledger-push-to-prod-2026-11-15.md,
// recorded about 14:40 America/Chicago. These are dated observations, not live budgets.
const rows = [
  ['Start and clone', '~10s', '11–12s · A/B', 'Four clones run in parallel; close to the design estimate.'],
  ['Select scope', '~5s', '32s · A; 7s · B', 'A spent 24.7s waiting for scope calculation. B returned full scope early because baseline evidence was missing.'],
  ['Backend tests', '20–25s for a leaf package', '57s · A; 66s · B; ~110–190s full suite', 'A tested 9 packages; B tested more than 10. Neither is a leaf-only sample. Shared dependencies and whole-API vet add time.'],
  ['Console tests', '0s for backend-only inputs; ~165s otherwise', '164s · A; 165s · B', 'A legitimately affected console inputs. B unexpectedly ran console tests after incremental baseline evidence was rejected.'],
  ['CI total', '~45s for a leaf backend change', '199s · A; 184s · B; 195s · C', 'No comparable leaf-only sample was measured. Parallel test durations must not be added.'],
  ['Merge after CI', '~10s', '15–18s', 'Polling adds a small delay.'],
  ['Main follow-up CI', '0s with reuse', '32s', 'A separate run starts only to reuse the result.'],
  ['Automatic delivery discovery', '~0s', '~30–60s', 'Delivery is discovered periodically.'],
  ['STG delivery', '~165s after the planned Pod reductions', '178–183s', 'Backend build 104–109s and console build 64–68s overlap; migration 16–17s, rollout 12–15s.'],
  ['PROD delivery', '~80s', '81–82s · 4 runs', 'Close to the estimate. This panel does not initiate delivery.'],
  ['Owner wait', '0s when policy allows', '0s when allowed; 14s–1h otherwise', 'Conditional on the release and its approval requirements.'],
] as const

export function TimingBudget() {
  return <OpsSection title="Designed vs actual" description="Historical benchmark · 15 Nov 2026, about 14:40 Chicago. Design estimates and measured samples have different scopes." bodyPadding="compact">
    <p className="m-0 mb-3 text-sm">Incremental merge was disabled at 14:31 that day. A and B were measured while it was enabled; these numbers do not describe the current configuration. No leaf-only probe was run.</p>
    <div className="mb-3 grid gap-3 sm:grid-cols-2">
      <div className="rounded bg-[var(--card-fill)] p-3"><p className="m-0 text-xs text-muted-foreground">Design scenario · estimate</p><p className="my-1 font-mono text-lg">~300s push to PROD</p><p className="m-0 text-xs">~55s to main. Assumes leaf backend CI ~45s, merge 10s, STG 165s, PROD 80s, with no extra waiting.</p></div>
      <div className="rounded bg-[var(--card-fill)] p-3"><p className="m-0 text-xs text-muted-foreground">Historical stages · combined estimate</p><p className="my-1 font-mono text-lg">~520–570s push to PROD</p><p className="m-0 text-xs">~200–217s to main. Combined from different measured runs, not one observed end-to-end delivery. Not a like-for-like regression delta.</p></div>
    </div>
    <div className="grid gap-2 lg:grid-cols-2">{rows.map(([step,designed,actual,gap]) => <article key={step} className="min-w-0 rounded bg-[var(--card-fill)] p-3">
      <h3 className="m-0 mb-2 text-sm font-medium">{step}</h3>
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <dt className="text-muted-foreground">Designed · estimate</dt><dd className="m-0">{designed}</dd>
        <dt className="text-muted-foreground">Measured</dt><dd className="m-0 font-mono">{actual}</dd>
      </dl><p className="mb-0 mt-2 text-xs text-muted-foreground">{gap}</p>
    </article>)}</div>
    <details className="mt-3 text-xs"><summary className="min-h-11 cursor-pointer py-3">Sources and comparability</summary>
      <ul className="m-0 space-y-2 pl-5 [overflow-wrap:anywhere]">
        <li>A: ci-platform-5stn2 / land-tz7pb · 1999f61b · one backend file also read by console declarations.</li>
        <li>B: ci-platform-4d828 / land-9c2mc · 3c40a33f · backend test deletion; console ran unexpectedly.</li>
        <li>C: bifrost-ci-platform-1794774424 · full main CI.</li>
        <li>Delivery examples: bifrost-deliver-platform-1794766308; bifrost-deliver-platform-prod-1794770848.</li>
        <li>Source ledger: benchmark/time-ledger-push-to-prod-2026-11-15.md. The ledger combines samples; it is not a current SLA. Use Run explorer above for retained live records.</li>
      </ul>
    </details>
  </OpsSection>
}
