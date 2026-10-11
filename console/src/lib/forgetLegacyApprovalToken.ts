/** Drop the retired page-level approval token. Nothing reads it back. */
export function forgetLegacyApprovalToken(): void {
  try {
    window.localStorage.removeItem('bifrost-ops-approval-token')
  } catch {
    // Private mode can throw. The key is still never read.
  }
}
