export interface ProgramDeliveryConfig {
  board_visible: boolean
  former_location?: string
  sign_off_mechanism?: 'api'
}

export interface ProgramSummary {
  id: string
  title: string
  label?: string
  description: string
  status: string
  lane_id?: string
  phase_count: number
  phases_done: number
  phases_signed?: number
  signed?: number
  complete?: boolean
  all_phases_done: boolean
  active: boolean
  former_location?: string
  sign_off_mechanism?: string
  delivery?: ProgramDeliveryConfig
}

export interface ProgramPhaseDetail {
  id: string
  title: string
  status: string
  signed_off?: boolean
  signed_off_at?: string
  signed_off_by?: string
  verify_cmd?: string
  acceptance?: string[]
  depends_on?: string[]
  sign_off?: { required: boolean; checklist?: string[] }
  agent_session?: { enabled: boolean }
  progress?: {
    phase_id: string
    status: string
    summary?: string
    verify_passed: boolean
    updated_at: string
  }
}

export interface AgentSessionRecord {
  id: string
  phase_id?: string
  program_id?: string
  started_at: string
  ended_at?: string
  cursor_agent_id?: string
  summary?: string
  track?: string
  lane?: string
  intent?: string
}

export interface PostCompletionItem {
  id: string
  program_id: string
  title: string
  description?: string
  status: 'pending_review' | 'approved' | 'rejected'
  created_at: string
  approved_at?: string
  approved_by?: string
}

export interface ProgramDetailResponse {
  program: ProgramSummary
  phases: ProgramPhaseDetail[]
  bridge?: {
    workspace: string
    model: string
    skill_path?: string
    skill_loaded: boolean
  }
  active: boolean
  agent_sessions?: AgentSessionRecord[]
  post_completion?: {
    submitted_at?: string
    new_capabilities?: string[]
    new_risks?: string[]
  }
  pending_post_completion_items?: PostCompletionItem[]
}

export interface ProgramsListResponse {
  programs: ProgramSummary[]
}

export interface LaunchProgramRequest {
  session_pack: string
  track?: string
  lane?: string
  intent?: string
  program_id?: string
  model?: string
  workspace?: string
}

export interface LaunchProgramResponse {
  agent_id?: string
  session_id: string
  status: string
  message?: string
}

export interface CreateProgramFromTemplateRequest {
  template_id: string
  instance_label?: string
  notes?: string
  lane_id?: string
}

export type DeliveryBoardProgramOverview = {
  id: string
  label: string
  description: string
  formerLocation: string
  phaseCount: number
  signed: number
  complete: boolean
  signOffMechanism?: string
  laneId?: string
}

export function mapProgramSummaryToOverview(p: ProgramSummary): DeliveryBoardProgramOverview {
  const signed = p.signed ?? p.phases_signed ?? 0
  return {
    id: p.id,
    label: p.label ?? p.title,
    description: p.description,
    formerLocation: p.former_location ?? p.delivery?.former_location ?? '',
    phaseCount: p.phase_count,
    signed,
    complete: p.complete ?? (p.phase_count > 0 && signed === p.phase_count),
    signOffMechanism: p.sign_off_mechanism ?? p.delivery?.sign_off_mechanism,
    laneId: p.lane_id,
  }
}
