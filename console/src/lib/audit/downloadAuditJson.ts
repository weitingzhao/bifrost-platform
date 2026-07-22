import type { AuditRecord, AuditResponse } from '@/api/auditTypes'

/** Client-side snapshot export — same shape as GET /api/v1/audit (+ export metadata). */
export function downloadAuditJson(records: AuditRecord[]): void {
  const payload: AuditResponse & { exported_at: string; source: string } = {
    exported_at: new Date().toISOString(),
    source: 'GET /api/v1/audit',
    records,
  }
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `platform-audit-${Date.now()}.json`
  a.click()
  URL.revokeObjectURL(url)
}
