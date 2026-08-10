import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchPatrolRuns,
  fetchPatrolSkills,
  type PatrolRun,
  type PatrolSkill,
} from '@/api/patrol'
import { PATROL_MOCK_RUNS, PATROL_MOCK_SKILLS } from '@/lib/patrol/patrolMock'
import {
  latestPatrolRun,
  patrolPosture,
  type PatrolPosture,
} from '@/lib/patrol/patrolStatus'

export const PATROL_SKILLS_QUERY_KEY = ['patrol', 'skills'] as const
export const PATROL_RUNS_QUERY_KEY = ['patrol', 'runs'] as const

function mockFallbackAllowed(): boolean {
  return import.meta.env.DEV === true && import.meta.env.VITE_PATROL_MOCK === '1'
}

export type PatrolSnapshot = {
  skills: PatrolSkill[]
  runs: PatrolRun[]
  posture: PatrolPosture
  latest: PatrolRun | undefined
  isLoading: boolean
  isError: boolean
  usedMock: boolean
}

export function usePatrolSnapshot(limit = 50): PatrolSnapshot {
  const skillsQ = useQuery({
    queryKey: PATROL_SKILLS_QUERY_KEY,
    queryFn: fetchPatrolSkills,
    refetchInterval: q =>
      q.state.data?.skills.some(s => s.last_result === 'running') ? 2_000 : 30_000,
    staleTime: 15_000,
  })
  const runsQ = useQuery({
    queryKey: [...PATROL_RUNS_QUERY_KEY, limit],
    queryFn: () => fetchPatrolRuns(limit),
    refetchInterval: q => (q.state.data?.runs.some(r => r.result === 'running') ? 2_000 : 30_000),
    staleTime: 15_000,
  })

  return useMemo(() => {
    const failed = skillsQ.isError || runsQ.isError
    const useMock = failed && mockFallbackAllowed()
    const skills = useMock ? PATROL_MOCK_SKILLS : (skillsQ.data?.skills ?? [])
    const runs = useMock ? PATROL_MOCK_RUNS : (runsQ.data?.runs ?? [])
    return {
      skills,
      runs,
      posture: patrolPosture(skills, runs),
      latest: latestPatrolRun(runs),
      isLoading: skillsQ.isLoading || runsQ.isLoading,
      isError: failed && !useMock,
      usedMock: useMock,
    }
  }, [
    skillsQ.data?.skills,
    skillsQ.isError,
    skillsQ.isLoading,
    runsQ.data?.runs,
    runsQ.isError,
    runsQ.isLoading,
  ])
}
