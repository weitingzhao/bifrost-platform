/**
 * The research version that is actually running, for anything that needs a tag.
 *
 * Three places used to reach for `RESEARCH_DEFAULT_TAG = '0.48.4'` instead: the
 * Launch Research tag box, the launch-desk checklist, and — worst — the Trade
 * page's Formation launch, which passes its tag straight to
 * `startPipelineRun('bifrost-deliver-research', …)`. Pressing Formation launch
 * would have built and rolled out research 0.48.4, a version from months before
 * the fleet reached 0.64. A constant cannot know what is deployed; this can.
 */
import { useQuery } from '@tanstack/react-query'
import { fetchResearchHealth, isResearchProxyError } from '@/api/researchEngine'
import { researchDefaultTag } from '@/lib/task-mode/researchLaunchVerdict'

/** Live research semver, or '' when it cannot be read. Never a made-up value. */
export function useResearchLiveTag(): { tag: string; isLoading: boolean } {
  const q = useQuery({
    queryKey: ['research', 'health', 'live-tag'],
    queryFn: fetchResearchHealth,
    refetchInterval: 60_000,
    staleTime: 30_000,
  })
  const health = q.data != null && !isResearchProxyError(q.data) ? q.data : null
  return { tag: researchDefaultTag(health?.version), isLoading: q.isLoading }
}
