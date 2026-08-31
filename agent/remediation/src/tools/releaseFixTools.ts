import type { SDKCustomTool } from '@cursor/sdk'
import { appendEvent, makeEvent, setPhase } from '../jobs.js'
import { jsonText } from '../platformClient.js'
import { textResult } from './helpers.js'

export function buildReleaseFixTools(jobId: string): Record<string, SDKCustomTool> {
  return {

    // ── Release-Fix escalation (Release Agent → Release-Fix Agent) ──

    spawn_release_fix: {
      description:
        'Escalate a release failure to a Release-Fix Agent. Starts a new agent task with scope "release-fix" that will ' +
        'attempt to diagnose and fix the root cause in the codebase. Returns the spawned job ID. ' +
        'After spawning, use poll_release_fix to wait for the result. ' +
        'Use this when a release phase fails (gate failure, build error, deploy error) and the error appears fixable in code/config.',
      inputSchema: {
        type: 'object',
        properties: {
          diagnosis: {
            type: 'string',
            description:
              'Detailed diagnosis report for the Release-Fix Agent. Include: which phase failed, the full error message/logs, ' +
              'what you believe the root cause is, which files/repos are likely involved, and any relevant context.',
          },
        },
        required: ['diagnosis'],
      },
      async execute(args) {
        const diagnosis = String(args.diagnosis ?? '')
        if (diagnosis.trim() === '') {
          return textResult('diagnosis must be a non-empty string describing the failure', true)
        }
        setPhase(jobId, 'awaiting_approval')
        appendEvent(jobId, makeEvent('status', 'Escalating to Release-Fix Agent…', {
          phase: 'awaiting_approval',
          escalation: 'release-fix',
        }))

        const runnerBase =
          process.env.REMEDIATION_RUNNER_URL?.replace(/\/$/, '') ??
          `http://127.0.0.1:${process.env.REMEDIATION_RUNNER_PORT ?? '8781'}`
        try {
          const resp = await fetch(`${runnerBase}/run`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              scope: 'release-fix',
              actor: 'release-agent',
              prompt: diagnosis,
            }),
          })
          if (!resp.ok) {
            const errText = await resp.text()
            setPhase(jobId, 'remediating')
            return textResult(`Failed to spawn release-fix: HTTP ${resp.status} ${errText}`, true)
          }
          const job = (await resp.json()) as { id: string; status: string }
          appendEvent(jobId, makeEvent('status', `Release-Fix Agent spawned: ${job.id}`, {
            release_fix_job_id: job.id,
          }))
          setPhase(jobId, 'remediating')
          return textResult(jsonText({ spawned: true, release_fix_job_id: job.id, status: job.status }))
        } catch (err) {
          setPhase(jobId, 'remediating')
          return textResult(`Failed to spawn release-fix: ${err instanceof Error ? err.message : String(err)}`, true)
        }
      },
    },


    poll_release_fix: {
      description:
        'Poll a Release-Fix Agent job for completion. Returns the job status, phase, summary, and error. ' +
        'Call this in a loop (every 15–30s) after spawn_release_fix until status is "done" or "failed". ' +
        'If done: the fix has been committed and pushed — you can retry the failed release phase. ' +
        'If failed: the fix could not be applied automatically — report to the operator for IDE Agent escalation.',
      inputSchema: {
        type: 'object',
        properties: {
          job_id: {
            type: 'string',
            description: 'The release-fix job ID returned by spawn_release_fix',
          },
        },
        required: ['job_id'],
      },
      async execute(args) {
        const fixJobId = String(args.job_id ?? '')
        if (fixJobId.trim() === '') {
          return textResult('job_id is required', true)
        }
        const runnerBase =
          process.env.REMEDIATION_RUNNER_URL?.replace(/\/$/, '') ??
          `http://127.0.0.1:${process.env.REMEDIATION_RUNNER_PORT ?? '8781'}`
        try {
          const resp = await fetch(`${runnerBase}/run/${encodeURIComponent(fixJobId)}`)
          if (!resp.ok) {
            const errText = await resp.text()
            return textResult(`Failed to poll release-fix job: HTTP ${resp.status} ${errText}`, true)
          }
          const job = (await resp.json()) as {
            id: string
            status: string
            phase: string
            summary?: string
            error?: string
          }
          return textResult(jsonText({
            job_id: job.id,
            status: job.status,
            phase: job.phase,
            summary: job.summary ?? null,
            error: job.error ?? null,
            completed: job.status !== 'running',
            fix_succeeded: job.status === 'done',
          }))
        } catch (err) {
          return textResult(`Failed to poll release-fix job: ${err instanceof Error ? err.message : String(err)}`, true)
        }
      },
    },

    // ── Trade Release-Fix escalation (trade-deploy / deliver-stg-recover → trade-release-fix) ──


    // ── Trade Release-Fix escalation (trade-deploy / deliver-stg-recover → trade-release-fix) ──

    spawn_trade_release_fix: {
      description:
        'Escalate a Trade deliver failure to Trade Release-Fix Agent. Starts scope "trade-release-fix" to patch bifrost-trade-infra / trade-* repos. ' +
        'Use when rollout/build/gitops failure requires GitOps manifest or code fix. Returns spawned job ID.',
      inputSchema: {
        type: 'object',
        properties: {
          diagnosis: {
            type: 'string',
            description: 'Detailed diagnosis: failing Tekton task, error logs, repos/files to fix.',
          },
        },
        required: ['diagnosis'],
      },
      async execute(args) {
        const diagnosis = String(args.diagnosis ?? '')
        if (diagnosis.trim() === '') {
          return textResult('diagnosis must be a non-empty string', true)
        }
        setPhase(jobId, 'awaiting_approval')
        appendEvent(jobId, makeEvent('status', 'Escalating to Trade Release-Fix Agent…', {
          phase: 'awaiting_approval',
          escalation: 'trade-release-fix',
        }))
        const runnerBase =
          process.env.REMEDIATION_RUNNER_URL?.replace(/\/$/, '') ??
          `http://127.0.0.1:${process.env.REMEDIATION_RUNNER_PORT ?? '8781'}`
        try {
          const resp = await fetch(`${runnerBase}/run`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              scope: 'trade-release-fix',
              actor: 'trade-deliver-agent',
              prompt: diagnosis,
            }),
          })
          if (!resp.ok) {
            const errText = await resp.text()
            setPhase(jobId, 'remediating')
            return textResult(`Failed to spawn trade-release-fix: HTTP ${resp.status} ${errText}`, true)
          }
          const job = (await resp.json()) as { id: string; status: string }
          appendEvent(jobId, makeEvent('status', `Trade Release-Fix spawned: ${job.id}`, {
            trade_release_fix_job_id: job.id,
          }))
          setPhase(jobId, 'remediating')
          return textResult(jsonText({ spawned: true, trade_release_fix_job_id: job.id, status: job.status }))
        } catch (err) {
          setPhase(jobId, 'remediating')
          return textResult(`Failed to spawn trade-release-fix: ${err instanceof Error ? err.message : String(err)}`, true)
        }
      },
    },


    poll_trade_release_fix: {
      description:
        'Poll a Trade Release-Fix job. Loop every 15–30s after spawn_trade_release_fix until done or failed.',
      inputSchema: {
        type: 'object',
        properties: {
          job_id: { type: 'string', description: 'Job ID from spawn_trade_release_fix' },
        },
        required: ['job_id'],
      },
      async execute(args) {
        const fixJobId = String(args.job_id ?? '')
        if (fixJobId.trim() === '') return textResult('job_id is required', true)
        const runnerBase =
          process.env.REMEDIATION_RUNNER_URL?.replace(/\/$/, '') ??
          `http://127.0.0.1:${process.env.REMEDIATION_RUNNER_PORT ?? '8781'}`
        try {
          const resp = await fetch(`${runnerBase}/run/${encodeURIComponent(fixJobId)}`)
          if (!resp.ok) {
            return textResult(`Failed to poll trade-release-fix: HTTP ${resp.status} ${await resp.text()}`, true)
          }
          const job = (await resp.json()) as {
            id: string
            status: string
            phase: string
            summary?: string
            error?: string
          }
          return textResult(jsonText({
            job_id: job.id,
            status: job.status,
            phase: job.phase,
            summary: job.summary ?? null,
            error: job.error ?? null,
            completed: job.status !== 'running',
            fix_succeeded: job.status === 'done',
          }))
        } catch (err) {
          return textResult(`Failed to poll trade-release-fix: ${err instanceof Error ? err.message : String(err)}`, true)
        }
      },
    },

    // ── Mutual watchdog tools (dual Mac Mini self-healing) ──

  }
}
