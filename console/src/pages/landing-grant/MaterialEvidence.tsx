import { CORE_LANDING_NOTICE, CORE_LANDING_TITLE, GITEA_NOTICE, STG_LANDING_NOTICE, giteaCompareHref, type LandingMaterialResponse } from '@/api/landingGrant'

export const PATCH_LIMIT = 512 * 1024

export function MaterialEvidence({ data }: { data: LandingMaterialResponse }) {
  const m = data.material, c = m.candidate
  const core = c.hard_core_paths.length > 0
  const compare = giteaCompareHref(m.compare_url)
  const patchFits = !m.patch_truncated && m.patch_size <= PATCH_LIMIT && new TextEncoder().encode(m.patch ?? '').length <= PATCH_LIMIT
  return <div className="space-y-4 text-sm [overflow-wrap:anywhere]">
    <section aria-label="Landing authorization" className="space-y-2">
      <h2 className="text-base font-semibold">{core ? CORE_LANDING_TITLE : 'Soft guardrail change'}</h2>
      {core && <p>{CORE_LANDING_NOTICE}</p>}
      {c.stg_release && <p>{STG_LANDING_NOTICE}</p>}
      <p>Main still M0? <strong>{data.main_known ? data.main_is_M0 ? 'Yes' : 'No — refresh required' : 'Unknown'}</strong> · Read at {m.computed_at}</p>
      <p>Main: <code>{data.main || 'Unavailable'}</code></p>
      <p>Quota today: {m.quota_reserved_today} reserved / {m.quota_max_per_day} maximum. Snapshot only; the quota at push time governs.</p>
      <p>Validity: 24 hours from the actual approval time; the grant below records its expiry.</p>
      <p>Phase: {m.phase} · Latest operation: {String(m.latest)} · Pinned: {String(m.pinned)} · Grants enabled: {String(m.grant_enabled)}</p>
      {m.problems.length > 0 && <p role="alert">Request cannot be made now: {m.problems.join(', ')}</p>}
    </section>
    <section aria-label="Digests and independent review" className="space-y-2">
      <p>Candidate digest: <code>{m.candidate_digest}</code></p>
      <p>Material digest: <code>{m.material_digest}</code></p>
      <p>Patch SHA-256: <code>{c.patch_sha256}</code></p>
      {compare ? <a className="underline" href={compare} target="_blank" rel="noopener noreferrer">Compare M0...R in Gitea</a> : <p>Gitea comparison link unavailable.</p>}
      <p>{c.decision_chain ? <strong>{GITEA_NOTICE}</strong> : GITEA_NOTICE}</p>
    </section>
    <section aria-label="Candidate identity"><h3 className="font-semibold">Candidate identity</h3><dl>
      {(['env', 'tenant', 'repo', 'mission', 'plan_step', 'task', 'attempt', 'op', 'A', 'base', 'R', 'M0', 'committer_date'] as const).map(key => <div key={key}><dt className="inline font-medium">{key}: </dt><dd className="inline">{c[key] || 'None'}</dd></div>)}
    </dl></section>
    <section aria-label="Checks and review" className="space-y-2">
      <h3 className="font-semibold">Checks and review</h3>
      <p>Soft checks at request: {m.soft_at_request.join(', ') || 'None'} · Waiting snapshot checks: {m.waiting_soft.join(', ') || 'None'}</p>
      <p>Rule: {m.rule.id} v{m.rule.version} · SHA-256: {m.rule.sha256} · Checks version: {m.checks_version}</p>
      <p>Acceptance:</p><ul>{c.acceptance.map(a => <li key={a.verification_id}>{a.verification_id}: {a.result}</li>)}</ul>
      <dl>{Object.entries(c.review).map(([key, value]) => <div key={key}><dt className="inline font-medium">Review {key}: </dt><dd className="inline">{value || 'None'}</dd></div>)}</dl>
      <p>A and R stable patch IDs match: {String(c.review_patch_match)}</p>
      <p>Decision chain: {String(c.decision_chain)} · Trust review answers present: {String(c.trust_review)}</p>
      {core && <ul aria-label="Trust-core paths">{c.hard_core_paths.map(p => <li key={p.path}>{p.path} · {p.entry}</li>)}</ul>}
    </section>
    <section aria-label="CI evidence"><h3 className="font-semibold">CI evidence</h3>
      <dl>{Object.entries(c.ci).filter(([k]) => k !== 'siblings').map(([key, value]) => <div key={key}><dt className="inline font-medium">{key}: </dt><dd className="inline">{String(value)}</dd></div>)}</dl>
      <ul>{Object.entries(c.ci.siblings).map(([repo, sha]) => <li key={repo}>{repo}: {sha}</li>)}</ul>
    </section>
    <section aria-label="Changed files"><h3 className="font-semibold">Changed files · {c.files} files · {c.lines} lines</h3>
      <div className="overflow-x-auto"><table className="w-full text-left"><thead><tr>{['Old path', 'New path', 'Old mode', 'New mode', 'Added', 'Deleted'].map(h => <th className="pr-3" key={h}>{h}</th>)}</tr></thead>
        <tbody>{c.changes.map((ch, i) => <tr key={i}>{[ch.old, ch.new, ch.old_mode, ch.new_mode, ch.added < 0 ? 'Binary' : ch.added, ch.deleted < 0 ? 'Binary' : ch.deleted].map((v, j) => <td className="pr-3 align-top" key={j}>{v === '' ? '—' : v}</td>)}</tr>)}</tbody></table></div>
    </section>
    <section aria-label="Patch"><h3 className="font-semibold">Patch · {m.patch_size} bytes</h3>
      {patchFits ? <pre className="max-h-[36rem] overflow-auto whitespace-pre text-xs">{m.patch || 'No patch body reported.'}</pre>
        : <p>Patch exceeds the 512 KB display limit. The complete file table is above; read the full patch using the Gitea comparison link. The digest covers the full patch.</p>}
    </section>
  </div>
}
