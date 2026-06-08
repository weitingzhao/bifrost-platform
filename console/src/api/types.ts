export type Reachability = 'ok' | 'degraded' | 'fail' | 'unknown'
export type AuthStatus = 'ok' | 'missing' | 'invalid' | 'skipped' | 'blocked'

export interface EnvironmentSummary {
  id: string
  label: string
}

export interface Target {
  id: string
  category: string
  reachability: Reachability
  auth: AuthStatus
  authorization_level: string
  detail: string
  url?: string
}

export interface MatrixResponse {
  environment: string
  label: string
  generated_at: string
  principal: { name: string; level: string }
  targets: Target[]
}

export interface AllMatricesResponse {
  matrices: MatrixResponse[]
}
