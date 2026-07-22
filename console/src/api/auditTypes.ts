export interface AuditRecord {
  id: string
  at: string
  actor: string
  role: 'viewer' | 'operator' | 'admin'
  action: string
  target: string
  status: string
  detail: string
}

export interface AuditResponse {
  records: AuditRecord[]
}
