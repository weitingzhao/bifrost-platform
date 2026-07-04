import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp, type DenseTagVariant } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { fetchDevAgentProgram } from '@/api/devAgent'
import type { DevAgentPhase } from '@/api/devAgentTypes'

function phaseVariant(status: DevAgentPhase['status']): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'running') return 'warning'
  if (status === 'failed') return 'danger'
  return 'neutral'
}

type DevAgentPhaseBoardProps = {
  programId: string
  phases: DevAgentPhase[]
}

export function DevAgentPhaseBoard({ programId, phases }: DevAgentPhaseBoardProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const detailQuery = useQuery({
    queryKey: ['dev-agent', 'program', programId],
    queryFn: () => fetchDevAgentProgram(programId),
    enabled: programId !== '',
  })

  const detailPhases = detailQuery.data?.phases ?? []
  const bridge = detailQuery.data?.bridge
  const phaseById = Object.fromEntries(
    detailPhases.map(p => [
      p.id,
      {
        acceptance: p.acceptance ?? [],
        verify_cmd: p.verify_cmd,
        rendered_prompt: p.rendered_prompt,
        skill_injected: p.skill_injected,
      },
    ]),
  )

  if (phases.length === 0) {
    return (
      <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">
        No phases returned from API. Check dev-agent handler initialization.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-2 px-3 py-2">
      {bridge && (
        <div className="flex flex-wrap items-center gap-2 rounded border border-border/40 bg-background/60 px-2 py-1.5">
          <span className="text-dense-caption font-medium text-muted-foreground uppercase tracking-wide">
            Bridge
          </span>
          <code className="font-mono text-dense-micro text-muted-foreground">{bridge.model}</code>
          <span className="text-dense-micro text-muted-foreground">·</span>
          <code className="max-w-[280px] truncate font-mono text-dense-micro text-muted-foreground">
            {bridge.workspace}
          </code>
          {bridge.skill_path && (
            <>
              <span className="text-dense-micro text-muted-foreground">·</span>
              <code className="max-w-[220px] truncate font-mono text-dense-micro text-muted-foreground">
                {bridge.skill_path}
              </code>
              <DenseTag variant={bridge.skill_loaded ? 'success' : 'warning'} className="text-dense-micro">
                skill {bridge.skill_loaded ? 'loaded' : 'missing'}
              </DenseTag>
            </>
          )}
        </div>
      )}

      {phases.map(p => {
        const expanded = expandedId === p.id
        const meta = phaseById[p.id]
        const acceptance = meta?.acceptance ?? []
        const hasDetail =
          acceptance.length > 0 ||
          (meta?.verify_cmd ?? '') !== '' ||
          (meta?.rendered_prompt ?? '') !== ''

        return (
          <div key={p.id} className="rounded border border-border/50 bg-secondary/10">
            <button
              type="button"
              className="flex w-full items-center gap-2 px-2 py-1.5 text-left hover:bg-secondary/30"
              onClick={() => setExpandedId(expanded ? null : p.id)}
              disabled={!hasDetail && !detailQuery.isLoading}
            >
              {hasDetail || detailQuery.isLoading ? (
                expanded ? (
                  <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                )
              ) : (
                <span className="w-3.5" />
              )}
              <StatusLamp
                value={
                  p.status === 'done'
                    ? 'ok'
                    : p.status === 'running'
                      ? 'degraded'
                      : p.status === 'failed'
                        ? 'fail'
                        : 'unknown'
                }
                kind="reach"
              />
              <span className="font-mono text-dense-label">{p.id}</span>
              <span className="flex-1 text-dense-meta text-muted-foreground truncate">
                {detailPhases.find(d => d.id === p.id)?.title ?? p.title}
              </span>
              <DenseTag variant={phaseVariant(p.status)} className="text-dense-micro">
                {p.status}
              </DenseTag>
            </button>
            {expanded && (
              <div className="border-t border-border/30 px-3 py-2 space-y-2">
                {detailQuery.isLoading ? (
                  <p className="m-0 text-dense-meta text-muted-foreground">Loading phase detail…</p>
                ) : (
                  <>
                    {acceptance.length === 0 ? (
                      <p className="m-0 text-dense-meta text-muted-foreground">
                        No acceptance criteria defined.
                      </p>
                    ) : (
                      <>
                        <p className="mb-1.5 text-dense-caption font-medium text-muted-foreground uppercase tracking-wide">
                          Acceptance
                        </p>
                        <ul className="m-0 list-none space-y-1 p-0">
                          {acceptance.map((item, i) => (
                            <li key={i} className="flex items-start gap-1.5 text-dense-meta">
                              <span className="mt-0.5 text-muted-foreground">•</span>
                              <span>{item}</span>
                            </li>
                          ))}
                        </ul>
                      </>
                    )}
                    {meta?.verify_cmd && (
                      <p className="m-0 text-dense-caption text-muted-foreground">
                        Verify:{' '}
                        <code className="font-mono text-dense-micro">{meta.verify_cmd}</code>
                      </p>
                    )}
                    {meta?.rendered_prompt && (
                      <div>
                        <p className="mb-1 flex items-center gap-2 text-dense-caption font-medium text-muted-foreground uppercase tracking-wide">
                          Rendered prompt
                          {meta.skill_injected && (
                            <DenseTag variant="success" className="text-dense-micro normal-case">
                              + skill
                            </DenseTag>
                          )}
                        </p>
                        <pre className="m-0 max-h-40 overflow-y-auto whitespace-pre-wrap rounded bg-background p-2 font-mono text-dense-micro text-muted-foreground">
                          {meta.rendered_prompt}
                        </pre>
                        {meta.skill_injected && (
                          <p className="mt-1 mb-0 text-dense-caption text-muted-foreground">
                            Bridge prepends skill file content before this template at execution time.
                          </p>
                        )}
                      </div>
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
