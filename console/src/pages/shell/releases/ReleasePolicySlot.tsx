/**
 * Release policy row in "Needs you". The platform does not serve a signed
 * release policy yet, so no release is auto-approved. Not counted in
 * "Needs you": it is not a request waiting for an answer.
 */
export function ReleasePolicySlot() {
  return (
    <div data-testid="release-policy-slot" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-xs">
      <span className="font-medium">No signed release policy</span>
      <span className="text-[var(--muted-foreground)]">Every production release waits for a manual approval.</span>
    </div>
  )
}
