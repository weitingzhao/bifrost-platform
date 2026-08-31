/** Shared helpers for remediation custom tools (no tool definitions). */
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { waitForOperatorResponse } from '../approvals.js'
import { appendEvent, makeEvent, setPhase } from '../jobs.js'
import { jsonText } from '../platformClient.js'

const execFileAsync = promisify(execFile)

export function kubeEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env }
  const kubeconfig = process.env.KUBECONFIG?.trim()
  if (kubeconfig != null && kubeconfig !== '') {
    env.KUBECONFIG = kubeconfig.replace(/^~/, process.env.HOME ?? '')
  }
  return env
}

export async function kubectl(args: string[], timeoutMs = 120_000): Promise<string> {
  const { stdout, stderr } = await execFileAsync('kubectl', args, {
    env: kubeEnv(),
    maxBuffer: 4 * 1024 * 1024,
    timeout: timeoutMs,
  })
  const out = stdout.trim()
  const err = stderr.trim()
  if (out === '' && err !== '') return err
  if (err !== '') return `${out}\n\nstderr:\n${err}`
  return out
}

// Resolve the SSH target of the peer agent host (the other Mac Mini).
// PEER_AGENT_SSH e.g. "vision@192.168.10.52". Used by the mutual watchdog
// to restart a downed peer runner.
export function peerSshTarget(explicit?: string): string | null {
  const candidate = (explicit ?? process.env.PEER_AGENT_SSH ?? '').trim()
  return candidate !== '' ? candidate : null
}

export async function ssh(target: string, remoteCmd: string, timeoutMs = 30_000): Promise<string> {
  const args = [
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=accept-new',
    '-o', 'ConnectTimeout=8',
    target,
    remoteCmd,
  ]
  const { stdout, stderr } = await execFileAsync('ssh', args, {
    env: process.env,
    maxBuffer: 1024 * 1024,
    timeout: timeoutMs,
  })
  const out = stdout.trim()
  const err = stderr.trim()
  if (out === '' && err !== '') return err
  if (err !== '') return `${out}\n\nstderr:\n${err}`
  return out
}

export function textResult(text: string, isError = false) {
  return { content: [{ type: 'text' as const, text }], isError }
}

export interface ApprovalOptionInput {
  id: string
  label: string
  description?: string
  destructive?: boolean
}

export const DEFAULT_MANUAL_STEP_OPTIONS: ApprovalOptionInput[] = [
  {
    id: 'manual_done',
    label: 'Done — continue repair',
    description: 'I finished the manual steps',
  },
  {
    id: 'manual_still_blocked',
    label: 'Still blocked',
    description: 'Steps did not resolve the issue',
  },
  {
    id: 'cancel',
    label: 'Stop task',
    description: 'End remediation without further action',
    destructive: true,
  },
]

export function parseApprovalOptions(raw: unknown): ApprovalOptionInput[] {
  if (!Array.isArray(raw)) return []
  return raw
    .filter((o): o is Record<string, unknown> => o != null && typeof o === 'object')
    .map(o => ({
      id: String(o.id ?? ''),
      label: String(o.label ?? o.id ?? 'Option'),
      description: o.description != null ? String(o.description) : undefined,
      destructive: o.destructive === true,
    }))
    .filter(o => o.id !== '')
}

export function parseStringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  return raw.map(String).filter(s => s.trim() !== '')
}

export function approvalShouldProceed(optionId: string): boolean {
  return optionId !== 'skip' && optionId !== 'cancel' && optionId !== 'stop'
}

export async function runOperatorApproval(
  jobId: string,
  params: {
    title: string
    message: string
    options: ApprovalOptionInput[]
    commands?: string[]
    checklist?: string[]
    kind?: 'manual_steps' | 'decision'
    note_hint?: string
    commit_message?: string
  },
) {
  const {
    title,
    message,
    options,
    commands = [],
    checklist = [],
    kind = 'decision',
    note_hint,
    commit_message,
  } = params

  if (options.length === 0) {
    return textResult('options must be a non-empty array', true)
  }

  setPhase(jobId, 'awaiting_approval')
  const meta: Record<string, unknown> = {
    title,
    options,
    commands,
    checklist,
    kind,
    note_hint,
  }
  if (commit_message != null && commit_message.trim() !== '') {
    meta.commit_message = commit_message.trim()
  }
  appendEvent(jobId, makeEvent('approval_request', message, meta))

  try {
    const decision = await waitForOperatorResponse(jobId)
    const statusText =
      decision.note != null && decision.note.trim() !== ''
        ? `Operator selected: ${decision.option_id} — ${decision.note.trim()}`
        : `Operator selected: ${decision.option_id}`
    appendEvent(
      jobId,
      makeEvent('status', statusText, {
        option_id: decision.option_id,
        note: decision.note,
        commit_message: decision.commit_message,
      }),
    )
    setPhase(jobId, 'remediating')
    const result: Record<string, unknown> = {
      selected: decision.option_id,
      note: decision.note ?? '',
      proceed: approvalShouldProceed(decision.option_id),
      still_blocked: decision.option_id === 'manual_still_blocked',
    }
    if (decision.commit_message != null && decision.commit_message.trim() !== '') {
      result.commit_message = decision.commit_message.trim()
    }
    return textResult(jsonText(result))
  } catch (err) {
    setPhase(jobId, 'remediating')
    return textResult(err instanceof Error ? err.message : String(err), true)
  }
}

