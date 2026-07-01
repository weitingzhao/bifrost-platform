import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchRemediationJobs, fetchVerifyMissionSnapshot } from '@/api/platform'
import type { RemediationJob, VerifyMissionSnapshotResponse } from '@/api/types'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { buildMissionVerifyMessage } from '@/lib/control-room/controlRoomOperatePack'

export type MissionVerifyBannerState = {
  jobId: string
  jobStatus: 'done' | 'failed'
  headline: string
  detail: string
  nominal: boolean
  postFixPassed: boolean | null
  payloadClassification: string | null
  verifiedAt: number
}

const COCKPIT_QUERY_PREFIX = 'cockpit'
const DISMISS_MS = 90_000

/**
 * Watches remediation jobs; when one transitions running → done/failed,
 * refreshes cockpit probes, runs verify_mission_snapshot reprobe, and exposes a verify banner.
 */
export function useMissionVerification(): {
  banner: MissionVerifyBannerState | null
  dismissBanner: () => void
  pendingVerify: boolean
} {
  const qc = useQueryClient()
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 5_000,
  })

  const statusByIdRef = useRef<Map<string, RemediationJob['status']>>(new Map())
  const [pendingJob, setPendingJob] = useState<{ id: string; status: 'done' | 'failed' } | null>(null)
  const [verifySnapshot, setVerifySnapshot] = useState<VerifyMissionSnapshotResponse | null>(null)
  const [snapshotLoading, setSnapshotLoading] = useState(false)
  const [banner, setBanner] = useState<MissionVerifyBannerState | null>(null)

  useEffect(() => {
    const jobs = jobsQuery.data?.jobs ?? []
    for (const job of jobs) {
      const prev = statusByIdRef.current.get(job.id)
      if (prev === 'running' && (job.status === 'done' || job.status === 'failed')) {
        setPendingJob({ id: job.id, status: job.status })
        setVerifySnapshot(null)
        void qc.invalidateQueries({ queryKey: [COCKPIT_QUERY_PREFIX] })
        setSnapshotLoading(true)
        void fetchVerifyMissionSnapshot()
          .then(data => {
            setVerifySnapshot(data)
            void qc.invalidateQueries({ queryKey: [COCKPIT_QUERY_PREFIX, 'verify-payload'] })
          })
          .catch(() => setVerifySnapshot(null))
          .finally(() => setSnapshotLoading(false))
      }
      statusByIdRef.current.set(job.id, job.status)
    }
  }, [jobsQuery.data, qc])

  useEffect(() => {
    if (pendingJob == null || missionLoading || snapshotLoading) return

    const msg = buildMissionVerifyMessage(snapshot, pendingJob.status, verifySnapshot ?? undefined)
    const postFix = verifySnapshot?.post_fix_verification
    setBanner({
      jobId: pendingJob.id,
      jobStatus: pendingJob.status,
      headline: msg.headline,
      detail: msg.detail,
      nominal: msg.nominal,
      postFixPassed: postFix?.passed ?? null,
      payloadClassification: verifySnapshot?.payload_verification.summary.overall ?? null,
      verifiedAt: Date.now(),
    })
    setPendingJob(null)

    const timer = window.setTimeout(() => setBanner(null), DISMISS_MS)
    return () => window.clearTimeout(timer)
  }, [pendingJob, missionLoading, snapshotLoading, snapshot, verifySnapshot])

  function dismissBanner() {
    setBanner(null)
  }

  return {
    banner,
    dismissBanner,
    pendingVerify: pendingJob != null || missionLoading || snapshotLoading,
  }
}
