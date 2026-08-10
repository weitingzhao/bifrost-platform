export const DEV_AGENT_PROGRAMS_QUERY_KEY = ['dev-agent', 'programs'] as const
export const DEV_AGENT_JOBS_QUERY_KEY = ['dev-agent', 'jobs'] as const

export function devAgentProgramQueryKey(programId: string) {
  return ['dev-agent', 'program', programId] as const
}

export function devAgentProgramJobsQueryKey(programId: string) {
  return ['dev-agent', 'jobs', programId] as const
}
