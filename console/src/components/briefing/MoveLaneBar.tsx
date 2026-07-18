import { useEffect, useState } from 'react'
import { Button, SegmentControl } from '@bifrost/ui'
import { Loader2 } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { LANES_QUERY_KEY, patchLane } from '@/api/lanes'
import {
  COMPONENT_LINE_SEGMENT_OPTIONS,
  componentLineById,
  trackTypeById,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import { defaultTrackForLine } from '@/lib/briefing/laneInitPack'
import type { WorkLane } from '@/lib/briefing/workLanes'

const TRACK_TYPE_OPTIONS: Array<{ value: WorkTrackType; label: string }> = [
  { value: 'build', label: 'Build' },
  { value: 'migrate', label: 'Migrate' },
  { value: 'maintain', label: 'Maintain' },
  { value: 'release', label: 'Release' },
]

export type MoveLaneBarProps = {
  lane: WorkLane
  canOperate: boolean
  /** Called after successful reclassification so the page can sync Scope / Track Type. */
  onMoved?: (line: ComponentLineId, trackType: WorkTrackType) => void
}

/**
 * Post-create reclassification — PATCH component_line / track_type when the Owner
 * realizes a lane was filed under the wrong Line.
 */
export function MoveLaneBar({ lane, canOperate, onMoved }: MoveLaneBarProps) {
  const [open, setOpen] = useState(false)
  const [line, setLine] = useState<ComponentLineId>(lane.componentLine)
  const [trackType, setTrackType] = useState<WorkTrackType>(lane.trackType)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [status, setStatus] = useState<string | null>(null)
  const qc = useQueryClient()

  // Reset editor when the selected lane changes (avoid stale classification UI).
  useEffect(() => {
    setOpen(false)
    setLine(lane.componentLine)
    setTrackType(lane.trackType)
    setError(null)
    setStatus(null)
  }, [lane.id, lane.componentLine, lane.trackType])

  const dirty = line !== lane.componentLine || trackType !== lane.trackType

  function handleOpen() {
    setLine(lane.componentLine)
    setTrackType(lane.trackType)
    setError(null)
    setStatus(null)
    setOpen(true)
  }

  async function handleSave() {
    if (!canOperate || !dirty) return
    setSubmitting(true)
    setError(null)
    setStatus(null)
    try {
      await patchLane(lane.id, {
        component_line: line,
        track_type: trackType,
        track: defaultTrackForLine(line),
      })
      await qc.invalidateQueries({ queryKey: LANES_QUERY_KEY })
      setStatus(
        `Moved to ${componentLineById(line).shortLabel} · ${trackTypeById(trackType).shortLabel}`,
      )
      setOpen(false)
      onMoved?.(line, trackType)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to move lane')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mt-2 rounded-md border border-[var(--border)]/60 bg-[var(--secondary)]/20 px-2.5 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)]">
          Classification
        </span>
        <span className="rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
          {componentLineById(lane.componentLine).shortLabel}
        </span>
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">·</span>
        <span className="rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
          {trackTypeById(lane.trackType).shortLabel}
        </span>
        {!open ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="ml-auto h-7"
            disabled={!canOperate}
            onClick={handleOpen}
            title={
              !canOperate
                ? 'Operator token required'
                : 'Reclassify this lane (component line / track type)'
            }
          >
            Move to…
          </Button>
        ) : (
          <button
            type="button"
            className="ml-auto text-[var(--text-dense-caption)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            onClick={() => setOpen(false)}
          >
            Cancel
          </button>
        )}
      </div>

      {open && (
        <div className="mt-2 flex flex-col gap-2">
          <div className="min-w-0">
            <p className="m-0 mb-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Component line
            </p>
            <SegmentControl
              value={line}
              onChange={v => setLine(v as ComponentLineId)}
              options={COMPONENT_LINE_SEGMENT_OPTIONS}
              size="xs"
              className="flex w-full min-w-0 max-w-full flex-wrap justify-start rounded-md"
            />
          </div>
          <div className="min-w-0">
            <p className="m-0 mb-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Track type
            </p>
            <SegmentControl
              value={trackType}
              onChange={v => setTrackType(v as WorkTrackType)}
              options={TRACK_TYPE_OPTIONS}
              size="xs"
              className="flex w-full min-w-0 max-w-full flex-wrap justify-start rounded-md"
            />
          </div>
          {error != null && (
            <p className="m-0 text-[var(--text-dense-caption)] text-destructive">{error}</p>
          )}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!canOperate || !dirty || submitting}
              onClick={() => void handleSave()}
            >
              {submitting ? (
                <>
                  <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> Saving…
                </>
              ) : (
                'Save classification'
              )}
            </Button>
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Writes config/lanes.yaml via PATCH
            </span>
          </div>
        </div>
      )}

      {status != null && !open && (
        <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--foreground)]">
          {status}
        </p>
      )}
    </div>
  )
}
