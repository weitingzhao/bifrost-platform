import { boardCloseTag, isProgramCatalogComplete } from '@/lib/briefing/programClose'

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
  /** Phases that require Owner sign-off (gates). Falls back to phase_count when absent. */
  sign_off_required_count?: number
  complete?: boolean
  all_phases_done: boolean
  active: boolean
  former_location?: string
  sign_off_mechanism?: string
  delivery?: ProgramDeliveryConfig
  /** Post-completion close state from programs API (no_handoff / closed / …). */
  assessment_status?: PostCompletionAssessmentStatus | string
  /** True when the program blueprint declares post_completion. */
  requires_post_completion?: boolean
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
  source_lane_id?: string
  operate_lane?: import('./operateQueueTypes').OperateLane | string
  handoff_kind?: import('./operateQueueTypes').HandoffKind
  reason?: string
  agent_task_id?: string
  acceptance_criteria?: string[]
  verification_steps?: string[]
  risk_level?: import('./operateQueueTypes').RiskLevel
  owner?: string
  due_at?: string
  status: 'pending_review' | 'approved' | 'rejected' | 'in_operate' | 'closed'
  created_at: string
  approved_at?: string
  approved_by?: string
  rejected_at?: string
  rejected_by?: string
  decision_note?: string
  execution_job_id?: string
  completion_evidence?: string[]
}

export type PostCompletionAssessmentStatus =
  | 'not_assessed'
  | 'no_handoff'
  | 'pending_review'
  | 'approved'
  | 'in_operate'
  | 'closed'

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
    assessed_at?: string
    assessed_by?: string
    assessment_status?: PostCompletionAssessmentStatus
    no_handoff_reason?: string
    suggested_assessment?: 'handoff' | 'no_handoff'
    suggested_items?: PostCompletionDraftItem[]
  }
  pending_post_completion_items?: PostCompletionItem[]
}

export interface PostCompletionDraftItem {
  id?: string
  source_lane_id?: string
  operate_lane?: import('./operateQueueTypes').OperateLane | string
  title: string
  description?: string
  handoff_kind?: import('./operateQueueTypes').HandoffKind
  reason?: string
  agent_task_id?: string
  acceptance_criteria?: string[]
  verification_steps?: string[]
  risk_level?: import('./operateQueueTypes').RiskLevel
  owner?: string
  due_at?: string
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

export type DeliveryBoardCloseTag = 'close_pending' | 'in_operate' | null

export type DeliveryBoardProgramOverview = {
  id: string
  label: string
  description: string
  formerLocation: string
  phaseCount: number
  phasesDone: number
  gateCount: number
  signed: number
  /** catalogComplete — not gates-only API `complete`. */
  complete: boolean
  closeTag: DeliveryBoardCloseTag
  assessmentStatus?: string
  requiresPostCompletion?: boolean
  signOffMechanism?: string
  laneId?: string
}

export function mapProgramSummaryToOverview(p: ProgramSummary): DeliveryBoardProgramOverview {
  const signed = p.signed ?? p.phases_signed ?? 0
  const gateCount = p.sign_off_required_count ?? p.phase_count
  const catalogComplete = isProgramCatalogComplete(p)
  const closeTag = boardCloseTag(p)
  return {
    id: p.id,
    label: p.label ?? p.title,
    description: p.description,
    formerLocation: p.former_location ?? p.delivery?.former_location ?? '',
    phaseCount: p.phase_count,
    phasesDone: p.phases_done,
    gateCount,
    signed,
    complete: catalogComplete,
    closeTag,
    assessmentStatus: p.assessment_status,
    requiresPostCompletion: p.requires_post_completion,
    signOffMechanism: p.sign_off_mechanism ?? p.delivery?.sign_off_mechanism,
    laneId: p.lane_id,
  }
}
