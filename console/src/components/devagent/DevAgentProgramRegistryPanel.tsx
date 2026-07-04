import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { activateDevAgentProgram, fetchDevAgentPrograms } from '@/api/devAgent'

export function DevAgentProgramRegistryPanel() {
  const qc = useQueryClient()
  const programsQuery = useQuery({
    queryKey: ['dev-agent', 'programs'],
    queryFn: fetchDevAgentPrograms,
    refetchInterval: 10_000,
  })

  const activateMutation = useMutation({
    mutationFn: (programId: string) => activateDevAgentProgram(programId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dev-agent'] }),
  })

  const programs = programsQuery.data?.programs ?? []

  return (
    <OpsSection
      title="Program Registry"
      description="Blueprint programs loaded from config/programs/*.yaml. Activate switches the Phase Board context."
      bodyPadding="compact"
    >
      {programsQuery.isLoading ? (
        <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">Loading programs…</p>
      ) : programsQuery.isError ? (
        <p className="m-0 px-3 py-2 text-dense-body text-destructive">
          Failed to load programs. Rebuild platform-api and restart.
        </p>
      ) : programs.length === 0 ? (
        <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">
          No programs loaded. Add a YAML file under config/programs/ and restart API.
        </p>
      ) : (
        <div className="flex flex-col gap-2 px-3 py-2">
          {programs.map(p => (
            <div
              key={p.id}
              className="flex flex-wrap items-center gap-2 rounded border border-border/50 bg-secondary/20 px-3 py-2"
            >
              <span className="font-mono text-dense-label">{p.id}</span>
              <span className="flex-1 text-dense-label">{p.title}</span>
              <DenseTag variant={p.status === 'active' ? 'success' : 'neutral'}>{p.status}</DenseTag>
              <span className="text-dense-meta text-muted-foreground">{p.phase_count} phases</span>
              {p.active ? (
                <DenseTag variant="warning">active</DenseTag>
              ) : (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={activateMutation.isPending}
                  onClick={() => activateMutation.mutate(p.id)}
                >
                  Activate
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </OpsSection>
  )
}
