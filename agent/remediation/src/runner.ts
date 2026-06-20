import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { Agent, CursorAgentError } from '@cursor/sdk'
import { appendEvent, finishJob, jobAbortSignal, makeEvent, setPhase } from './jobs.js'
import { buildRemediationPrompt } from './prompt.js'
import { buildCustomTools } from './tools.js'
import type { StartRunRequest } from './types.js'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

function defaultCwd(): string {
  if (process.env.REMEDIATION_CWD?.trim()) {
    return process.env.REMEDIATION_CWD.replace(/^~/, process.env.HOME ?? '')
  }
  return path.resolve(__dirname, '../../../../bifrost-trade-infra')
}

function stringifyUnknown(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export async function runRemediationJob(jobId: string, req: StartRunRequest): Promise<void> {
  const apiKey = process.env.CURSOR_API_KEY?.trim()
  if (apiKey == null || apiKey === '') {
    finishJob(jobId, 'failed', 'CURSOR_API_KEY is not configured.', 'missing CURSOR_API_KEY')
    return
  }

  const cwd = defaultCwd()
  const prompt = buildRemediationPrompt(req)
  const customTools = buildCustomTools()
  const modelId = process.env.REMEDIATION_MODEL?.trim() || 'composer-2.5'

  appendEvent(jobId, makeEvent('status', `Starting agent (cwd=${cwd})`))
  setPhase(jobId, 'diagnosing')

  let agent: Awaited<ReturnType<typeof Agent.create>> | undefined
  try {
    agent = await Agent.create({
      apiKey,
      model: { id: modelId },
      local: { cwd, customTools },
    })

    const run = await agent.send(prompt)
    setPhase(jobId, 'remediating')

    for await (const event of run.stream()) {
      if (jobAbortSignal(jobId)?.aborted) {
        if (run.supports('cancel')) await run.cancel()
        return
      }

      switch (event.type) {
        case 'thinking':
          if (event.text.trim() !== '') {
            appendEvent(jobId, makeEvent('thinking', event.text))
          }
          break
        case 'assistant':
          for (const block of event.message.content) {
            if (block.type === 'text' && block.text.trim() !== '') {
              appendEvent(jobId, makeEvent('thinking', block.text))
            }
          }
          break
        case 'tool_call': {
          const payload = stringifyUnknown(event.args ?? {})
          if (event.status === 'completed' || event.result != null) {
            appendEvent(
              jobId,
              makeEvent('tool_result', stringifyUnknown(event.result ?? ''), {
                name: event.name,
                call_id: event.call_id,
              }),
            )
          } else {
            appendEvent(
              jobId,
              makeEvent('tool_call', `${event.name} ${payload}`, {
                name: event.name,
                call_id: event.call_id,
                args: event.args,
              }),
            )
          }
          break
        }
        case 'status':
          appendEvent(jobId, makeEvent('status', event.status))
          break
        default:
          break
      }
    }

    setPhase(jobId, 'verifying')
    const result = await run.wait()
    if (result.status === 'error') {
      finishJob(jobId, 'failed', 'Agent run failed.', result.result ?? 'run error')
      return
    }
    finishJob(jobId, 'done', result.result?.trim() || 'Remediation completed.')
  } catch (err) {
    if (jobAbortSignal(jobId)?.aborted) return
    const message = err instanceof CursorAgentError ? err.message : err instanceof Error ? err.message : String(err)
    appendEvent(jobId, makeEvent('error', message))
    finishJob(jobId, 'failed', 'Remediation failed.', message)
  } finally {
    if (agent != null) {
      try {
        await agent[Symbol.asyncDispose]()
      } catch {
        // ignore dispose errors
      }
    }
  }
}
