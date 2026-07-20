import { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  SegmentControl,
} from '@bifrost/ui'
import { Loader2, Plus } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { createLane, LANES_QUERY_KEY } from '@/api/lanes'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { classifyLane } from '@/lib/briefing/laneClassifier'
import {
  buildNewLaneInitPack,
  defaultTrackForLine,
  slugLaneId,
} from '@/lib/briefing/laneInitPack'
import {
  COMPONENT_LINE_IDS,
  COMPONENT_LINE_SEGMENT_OPTIONS,
  componentLineById,
  trackTypeById,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'

const TRACK_TYPE_OPTIONS: Array<{ value: WorkTrackType; label: string }> = [
  { value: 'build', label: 'Build' },
  { value: 'migrate', label: 'Migrate' },
  { value: 'maintain', label: 'Maintain' },
  { value: 'release', label: 'Release' },
]

export type NewLaneDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called after successful create with the new lane id + chosen classification. */
  onCreated?: (laneId: string, line: ComponentLineId, trackType: WorkTrackType) => void
}

/**
 * Top-level New Lane flow: describe work first, then confirm recommended Line / Track Type.
 * Does not depend on the current Briefing Scope filter.
 */
export function NewLaneDialog({ open, onOpenChange, onCreated }: NewLaneDialogProps) {
  const [label, setLabel] = useState('')
  const [description, setDescription] = useState('')
  /** When set, Owner overrode auto recommendation for that axis. */
  const [lineOverride, setLineOverride] = useState<ComponentLineId | null>(null)
  const [trackTypeOverride, setTrackTypeOverride] = useState<WorkTrackType | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()

  const recommendation = useMemo(
    () => classifyLane({ label, description }),
    [label, description],
  )

  const line = lineOverride ?? recommendation.line
  const trackType = trackTypeOverride ?? recommendation.trackType

  useEffect(() => {
    if (!open) return
    setLabel('')
    setDescription('')
    setLineOverride(null)
    setTrackTypeOverride(null)
    setError(null)
    setSubmitting(false)
  }, [open])

  async function handleCreate() {
    const trimmedLabel = label.trim()
    const trimmedDesc = description.trim()
    if (trimmedLabel === '' || trimmedDesc === '') return
    if (!canOperate) {
      setError('Authenticate as operator to create a lane')
      return
    }
    const id = slugLaneId(trimmedLabel)
    if (id.length < 2) {
      setError('Label must yield a valid kebab-case id')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const created = await createLane({
        id,
        track: defaultTrackForLine(line),
        component_line: line,
        track_type: trackType,
        label: trimmedLabel,
        short_label: trimmedLabel.slice(0, 24),
        description: trimmedDesc,
        agent_mode: 'Ops',
        work_intent: 'feature',
      })
      await qc.invalidateQueries({ queryKey: LANES_QUERY_KEY })
      const pack = buildNewLaneInitPack(line, trackType, trimmedDesc, created.id)
      try {
        await navigator.clipboard.writeText(pack)
      } catch {
        // clipboard optional
      }
      onCreated?.(created.id, line, trackType)
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create lane')
    } finally {
      setSubmitting(false)
    }
  }

  const confidenceHint =
    recommendation.confidence === 'low'
      ? 'Low confidence — please confirm Line and Track Type manually.'
      : recommendation.confidence === 'high'
        ? 'High confidence recommendation — adjust if needed.'
        : 'Suggested classification — review before creating.'

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New Lane</DialogTitle>
          <DialogDescription>
            Describe the work direction first. We recommend a component line and track type — you
            can change them before creating.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 py-1">
          <div>
            <p className="m-0 mb-1 text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)]">
              1. Describe the work
            </p>
            <input
              className="w-full rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:border-[var(--primary)] focus:outline-none"
              placeholder="Lane label (becomes kebab-case id)"
              value={label}
              onChange={e => setLabel(e.target.value)}
              autoFocus
            />
            <textarea
              className="mt-2 w-full resize-none rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:border-[var(--primary)] focus:outline-none"
              rows={3}
              placeholder="What problem does this solve? What will it deliver?"
              value={description}
              onChange={e => setDescription(e.target.value)}
            />
          </div>

          <div className="rounded-md border border-[var(--border)]/70 bg-[var(--secondary)]/30 px-3 py-2.5">
            <p className="m-0 mb-1 text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)]">
              2. Confirm classification
            </p>
            <p
              className={[
                'm-0 mb-2 text-[var(--text-dense-meta)]',
                recommendation.confidence === 'low'
                  ? 'text-[var(--warning)]'
                  : 'text-[var(--muted-foreground)]',
              ].join(' ')}
            >
              {confidenceHint}
              {recommendation.reason !== '' ? ` ${recommendation.reason}` : ''}
            </p>

            <p className="m-0 mb-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Component line
            </p>
            <SegmentControl
              value={line}
              onChange={v => {
                setLineOverride(v as ComponentLineId)
              }}
              options={COMPONENT_LINE_SEGMENT_OPTIONS}
              size="xs"
              className="flex w-full min-w-0 max-w-full flex-wrap justify-start rounded-md"
            />

            <p className="m-0 mb-1 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Track type
            </p>
            <SegmentControl
              value={trackType}
              onChange={v => {
                setTrackTypeOverride(v as WorkTrackType)
              }}
              options={TRACK_TYPE_OPTIONS}
              size="xs"
              className="flex w-full min-w-0 max-w-full flex-wrap justify-start rounded-md"
            />

            <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Will create under{' '}
              <span className="font-medium text-[var(--foreground)]">
                {componentLineById(line).shortLabel} · {trackTypeById(trackType).shortLabel}
              </span>
              {label.trim() !== '' ? (
                <>
                  {' '}
                  as <code className="text-[var(--foreground)]">{slugLaneId(label) || '…'}</code>
                </>
              ) : null}
            </p>
          </div>

          {error != null && (
            <p className="m-0 text-[var(--text-dense-caption)] text-destructive">{error}</p>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={
              submitting ||
              !canOperate ||
              label.trim() === '' ||
              description.trim() === '' ||
              !COMPONENT_LINE_IDS.includes(line)
            }
            onClick={() => void handleCreate()}
            title={!canOperate ? 'Operator token required' : undefined}
          >
            {submitting ? (
              <>
                <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> Creating…
              </>
            ) : (
              <>
                <Plus className="mr-1 h-3.5 w-3.5" /> Create lane
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
