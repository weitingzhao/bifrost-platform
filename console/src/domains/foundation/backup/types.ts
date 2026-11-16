export interface ClusterPostgresBackupStatusResponse {
  fresh: boolean
  signal: string
  detail: string
  last_completed_at?: string
  last_backup_name?: string
  last_backup_phase?: string
  max_age_hours: number
  age_hours?: number
  backup_count: number
  stuck_backups?: string[]
  wal_archiving_ok?: boolean
  wal_archiving_detail?: string
  generated_at: string
}

