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

/**
 * Remediation jobs are retired. The banner stays in the type so Mission Control
 * can still render a verify result if one is supplied later; nothing watches jobs.
 */
export function useMissionVerification(): {
  banner: MissionVerifyBannerState | null
  dismissBanner: () => void
  pendingVerify: boolean
} {
  return {
    banner: null,
    dismissBanner() {},
    pendingVerify: false,
  }
}
