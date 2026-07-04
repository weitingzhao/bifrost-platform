import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchDevAgentPersistence } from '@/api/devAgent'

export function DevAgentPersistencePanel() {
  const persistenceQuery = useQuery({
    queryKey: ['dev-agent', 'persistence'],
    queryFn: fetchDevAgentPersistence,
    refetchInterval: 15_000,
  })

  const info = persistenceQuery.data

  return (
    <OpsSection
      title="State Persistence"
      description="Phase status, active jobs, and history are stored as JSON under data/dev-agent/ and survive API restarts."
      bodyPadding="compact"
    >
      {persistenceQuery.isLoading ? (
        <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">Loading persistence info…</p>
      ) : persistenceQuery.isError ? (
        <p className="m-0 px-3 py-2 text-dense-body text-destructive">
          Failed to load persistence info. Rebuild and restart platform-api.
        </p>
      ) : info ? (
        <div className="flex flex-col gap-2 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2 text-dense-meta">
            <span className="text-muted-foreground">State dir:</span>
            <code className="font-mono text-dense-caption">{info.state_dir}</code>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-dense-meta">
            <span className="text-muted-foreground">Active program:</span>
            <span className="font-mono text-dense-label">{info.active_program_id}</span>
          </div>
          {info.files.length === 0 ? (
            <p className="m-0 text-dense-meta text-muted-foreground">
              No state files yet — start or approve a phase to create one.
            </p>
          ) : (
            info.files.map(f => (
              <div
                key={f.program_id}
                className="flex flex-wrap items-center gap-2 rounded border border-border/40 bg-background px-2 py-1.5"
              >
                <span className="font-mono text-dense-label">{f.program_id}</span>
                <DenseTag variant="neutral">{f.bytes} B</DenseTag>
                {f.updated_at && (
                  <span className="text-dense-caption text-muted-foreground">{f.updated_at}</span>
                )}
                <code className="flex-1 truncate font-mono text-dense-micro text-muted-foreground">
                  {f.path}
                </code>
              </div>
            ))
          )}
        </div>
      ) : null}
    </OpsSection>
  )
}
