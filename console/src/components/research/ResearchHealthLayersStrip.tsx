import { StatusLamp, cn } from '@bifrost/ui'
import {
  layerVerdictToLamp,
  type ResearchHealthLayerView,
} from '@/lib/research/researchHealthCopy'

type Props = {
  layers: ResearchHealthLayerView[]
  className?: string
  onSelectLayer?: (id: ResearchHealthLayerView['id']) => void
}

/** Always-on three-layer health strip — not buried in a tab. */
export function ResearchHealthLayersStrip({ layers, className, onSelectLayer }: Props) {
  return (
    <ul
      className={cn(
        'm-0 grid list-none grid-cols-1 gap-1.5 p-0 sm:grid-cols-3',
        className,
      )}
      aria-label="Research health layers"
    >
      {layers.map(layer => (
        <li key={layer.id}>
          {onSelectLayer != null ? (
            <button
              type="button"
              className="flex w-full items-start gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2.5 py-2 text-left hover:bg-[var(--card-fill-hover)]"
              onClick={() => onSelectLayer(layer.id)}
            >
              <LayerBody layer={layer} />
            </button>
          ) : (
            <div className="flex w-full items-start gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2.5 py-2">
              <LayerBody layer={layer} />
            </div>
          )}
        </li>
      ))}
    </ul>
  )
}

function LayerBody({ layer }: { layer: ResearchHealthLayerView }) {
  return (
    <>
      <span className="mt-0.5 shrink-0">
        <StatusLamp value={layerVerdictToLamp(layer.verdict)} kind="reach" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-1.5">
          <span className="text-dense-label font-medium text-foreground">{layer.label}</span>
          <span className="font-mono text-dense-micro uppercase text-muted-foreground">
            {layer.verdict}
          </span>
        </span>
        <span className="mt-0.5 block text-dense-caption text-muted-foreground">{layer.meta}</span>
      </span>
    </>
  )
}
