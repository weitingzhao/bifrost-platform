import { DenseTag } from '@bifrost/ui'
import type { DockerfileConfigMapView } from '@/api/types'
import { EXPECTED_DOCKERFILE_CONFIGMAPS } from '@/lib/delivery/deliverStgPhases'

interface DockerfileCmSummaryProps {
  configmaps: DockerfileConfigMapView[] | undefined
  loading?: boolean
}

export function DockerfileCmSummary({ configmaps, loading = false }: DockerfileCmSummaryProps) {
  if (loading) {
    return (
      <p className="m-0 mt-3 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Dockerfile ConfigMaps: loading…
      </p>
    )
  }

  const presentCount = EXPECTED_DOCKERFILE_CONFIGMAPS.filter(exp =>
    configmaps?.some(cm => cm.name === exp.name && cm.present),
  ).length
  const allOk = presentCount === EXPECTED_DOCKERFILE_CONFIGMAPS.length

  return (
    <div className="mt-3 rounded-md border border-[var(--border)] bg-[var(--secondary)]/30 px-3 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-label)] font-medium">
          Dockerfile ConfigMaps: {presentCount}/{EXPECTED_DOCKERFILE_CONFIGMAPS.length}
        </span>
        <DenseTag variant={allOk ? 'success' : 'warning'}>{allOk ? 'ready for build' : 'incomplete'}</DenseTag>
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        {EXPECTED_DOCKERFILE_CONFIGMAPS.map(exp => {
          const cm = configmaps?.find(c => c.name === exp.name)
          const ok = cm?.present === true
          return (
            <DenseTag key={exp.name} variant={ok ? 'success' : 'danger'} title={exp.name}>
              {exp.short}
            </DenseTag>
          )
        })}
      </div>
      {!allOk && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Run Refresh Dockerfile CMs before deliver-stg. Full table: Observe → Supply chain — inventory.
        </p>
      )}
    </div>
  )
}
