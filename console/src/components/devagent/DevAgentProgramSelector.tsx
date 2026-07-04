import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { SegmentControl } from '@bifrost/ui'
import { activateDevAgentProgram, fetchDevAgentPrograms } from '@/api/devAgent'
import type { DevAgentProgramSummary } from '@/api/devAgentTypes'

type DevAgentProgramSelectorProps = {
  activeProgramId: string | undefined
}

function programLabel(p: DevAgentProgramSummary): string {
  if (p.all_phases_done) {
    return `${p.title} (done)`
  }
  return p.title
}

export function DevAgentProgramSelector({ activeProgramId }: DevAgentProgramSelectorProps) {
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
  const selectedId = activeProgramId ?? programs.find(p => p.active)?.id ?? programs[0]?.id ?? ''

  if (programsQuery.isLoading) {
    return <span className="text-dense-meta text-muted-foreground">Loading programs…</span>
  }

  if (programsQuery.isError || programs.length === 0) {
    return null
  }

  if (programs.length <= 5) {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-dense-meta text-muted-foreground shrink-0">Program:</span>
        <SegmentControl
          value={selectedId}
          onChange={id => {
            if (id !== selectedId && !activateMutation.isPending) {
              activateMutation.mutate(id)
            }
          }}
          options={programs.map(p => ({
            value: p.id,
            label: programLabel(p),
          }))}
          size="sm"
        />
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-dense-meta text-muted-foreground shrink-0">Program:</span>
      <select
        className="h-7 rounded border border-border bg-background px-2 text-dense-label"
        value={selectedId}
        onChange={e => {
          const id = e.target.value
          if (id !== selectedId) activateMutation.mutate(id)
        }}
        disabled={activateMutation.isPending}
      >
        {programs.map(p => (
          <option key={p.id} value={p.id}>
            {programLabel(p)}
          </option>
        ))}
      </select>
    </div>
  )
}
