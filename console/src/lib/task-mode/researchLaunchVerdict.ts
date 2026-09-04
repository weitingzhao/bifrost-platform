import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'

/** Semver pin required by Launch Research (must match the Kaniko tag). */
export const RESEARCH_TAG_RE = /^\d+\.\d+\.\d+$/

/**
 * Format hint for the empty tag box — an example of the shape, never a value
 * anyone should launch.
 *
 * This used to be `RESEARCH_DEFAULT_TAG = '0.48.4'`, pre-filled into the input
 * and read straight back out by the lane's step derivation, which calls the
 * manifest pinned only when `liveVersion === tag`. Nobody had typed 0.48.4 in
 * months, so the desk sat at "Pin manifest 2/4" forever, describing a release
 * of a version that was never being released — while the real fleet moved from
 * 0.48 to 0.64. A stale constant that renders in a state field is worse than a
 * blank one: it reads as fact.
 */
export const RESEARCH_TAG_PLACEHOLDER = '0.0.0'

/**
 * What the tag box should start on: what is running now. The operator edits it
 * to the version they intend to ship, so the box always begins at a true
 * statement, and an empty box means "we could not read the live version" rather
 * than a number from last quarter.
 */
export function researchDefaultTag(liveVersion?: string | null): string {
  const v = (liveVersion ?? '').trim()
  return isResearchReleaseTag(v) ? v : ''
}

export function isResearchReleaseTag(tag: string): boolean {
  return RESEARCH_TAG_RE.test(tag.trim())
}

export type ResolveResearchLaunchVerdictInput = {
  canOperate: boolean
  pipelinePresent: boolean
  tag: string
  deliverInFlight: boolean
  agentInFlight?: boolean
}

export function buildResearchLaunchCheckpoints(
  input: ResolveResearchLaunchVerdictInput,
): LaunchCheckpoint[] {
  const tagOk = isResearchReleaseTag(input.tag)
  return [
    { id: 'auth', label: 'Operator auth', ok: input.canOperate },
    {
      id: 'pipeline',
      label: 'Research pipeline',
      ok: input.pipelinePresent,
      detail: input.pipelinePresent ? undefined : 'bifrost-deliver-research missing',
      readinessAnchor: 'pipeline',
    },
    {
      id: 'tag',
      label: 'Image tag',
      ok: tagOk,
      detail: tagOk ? input.tag.trim() : 'semver required (e.g. 1.2.3)',
    },
  ]
}

export function resolveResearchLaunchVerdict(
  input: ResolveResearchLaunchVerdictInput,
): LaunchVerdict {
  if (input.deliverInFlight || input.agentInFlight) {
    return {
      kind: 'IN_FLIGHT',
      title: 'Research launch in flight',
      detail: input.agentInFlight
        ? 'AI Deploy Research is running — decide in Agent Session, then watch Build steps.'
        : 'bifrost-deliver-research is running — wait for Kaniko + verify.',
      disabledReason: 'Research delivery already running',
    }
  }
  if (!input.canOperate) {
    return {
      kind: 'NO_GO',
      title: 'Authenticate to launch',
      detail: 'Operator token required for Launch Research.',
      disabledReason: 'Authenticate as operator to launch Research',
      blockKind: 'auth',
    }
  }
  if (!input.pipelinePresent) {
    return {
      kind: 'NO_GO',
      title: 'Research pipeline missing',
      detail: 'Apply k8s/cicd/tekton/pipeline-deliver-research.yaml.',
      disabledReason: 'Research delivery pipeline not installed',
    }
  }
  if (!isResearchReleaseTag(input.tag)) {
    return {
      kind: 'NO_GO',
      title: 'Image tag required',
      detail: 'Pass the version to build (e.g. 0.48.4). Default `dev` must not pin k8s.',
      disabledReason: 'Enter a semver image tag before Launch Research',
    }
  }
  return {
    kind: 'GO',
    title: 'Ready to publish Research',
    detail: `Build ${input.tag.trim()} first, then bump k8s/api/deployment.yaml after the image lands.`,
  }
}
