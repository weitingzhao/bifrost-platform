/**
 * Cursor SDK Bridge — called by Platform API to orchestrate dev agent tasks.
 *
 * Usage:
 *   node bridge.js --prompt "..." --phase PHASE-0 --workspace /path --model composer-2.5 --skill-path .cursor/skills/foo/SKILL.md
 *   node bridge.js --dry-run ...   # print composed prompt, no SDK call
 *   node bridge.js --resume <agent-id> --prompt "Fix X" --workspace /path --model composer-2.5
 *
 * Requires: CURSOR_API_KEY env var (except --dry-run)
 */

import { readFileSync } from 'node:fs'
import { isAbsolute, join } from 'node:path'
import { parseArgs } from 'node:util'
import { Agent } from '@cursor/sdk'

const DEFAULT_WORKSPACE = '/Users/vision-mac-trader/Desktop/stocks'
const DEFAULT_MODEL = 'composer-2.5'

const { values } = parseArgs({
  options: {
    prompt: { type: 'string' },
    phase: { type: 'string' },
    resume: { type: 'string' },
    workspace: { type: 'string' },
    model: { type: 'string' },
    'skill-path': { type: 'string' },
    'dry-run': { type: 'boolean', default: false },
  },
})

export function resolveSkillPath(workspace: string, skillPath: string): string {
  if (isAbsolute(skillPath)) return skillPath
  return join(workspace, skillPath)
}

export function buildFinalPrompt(
  basePrompt: string,
  workspace: string,
  skillPath?: string,
): { prompt: string; skillInjected: boolean; skillResolved?: string } {
  if (!skillPath?.trim()) {
    return { prompt: basePrompt, skillInjected: false }
  }
  const resolved = resolveSkillPath(workspace, skillPath.trim())
  try {
    const skill = readFileSync(resolved, 'utf-8')
    const prompt = `# Skill context (${skillPath})\n\n${skill}\n\n---\n\n${basePrompt}`
    return { prompt, skillInjected: true, skillResolved: resolved }
  } catch {
    console.error(`[bridge] skill file not found: ${resolved}`)
    return { prompt: basePrompt, skillInjected: false, skillResolved: resolved }
  }
}

async function main() {
  const basePrompt = values.prompt
  if (!basePrompt) {
    console.error('[bridge] --prompt is required')
    process.exit(1)
  }

  const workspace = values.workspace?.trim() || DEFAULT_WORKSPACE
  const model = values.model?.trim() || DEFAULT_MODEL
  const skillPath = values['skill-path']?.trim()
  const { prompt, skillInjected, skillResolved } = buildFinalPrompt(
    basePrompt,
    workspace,
    skillPath,
  )

  if (values['dry-run']) {
    console.log('[bridge] dry-run')
    console.log(`workspace=${workspace}`)
    console.log(`model=${model}`)
    console.log(`phase=${values.phase ?? ''}`)
    console.log(`skill-path=${skillPath ?? ''}`)
    console.log(`skill-resolved=${skillResolved ?? ''}`)
    console.log(`skill-injected=${skillInjected}`)
    console.log('--- PROMPT ---')
    console.log(prompt)
    return
  }

  const apiKey = process.env.CURSOR_API_KEY
  if (!apiKey) {
    console.error('[bridge] CURSOR_API_KEY not set')
    process.exit(1)
  }

  const resumeId = values.resume

  if (resumeId) {
    await using agent = await Agent.resume(resumeId, { apiKey })
    const run = await agent.send(prompt)
    for await (const event of run.stream()) {
      if (event.type === 'assistant') {
        for (const block of event.message.content) {
          if (block.type === 'text') process.stdout.write(block.text)
        }
      }
    }
    const result = await run.wait()
    if (result.status === 'error') {
      console.error('\n[bridge] Agent run failed:', result.id)
      process.exit(2)
    }
  } else {
    await using agent = await Agent.create({
      apiKey,
      model: { id: model },
      local: { cwd: workspace },
    })

    console.log(`[bridge] agent_id=${agent.agentId}`)
    console.log(`[bridge] phase=${values.phase ?? 'unknown'}`)
    console.log(`[bridge] workspace=${workspace}`)
    console.log(`[bridge] model=${model}`)
    console.log(`[bridge] skill-injected=${skillInjected}`)
    console.log(`[bridge] starting execution...\n`)

    const run = await agent.send(prompt)
    for await (const event of run.stream()) {
      if (event.type === 'assistant') {
        for (const block of event.message.content) {
          if (block.type === 'text') process.stdout.write(block.text)
        }
      }
    }
    const result = await run.wait()
    if (result.status === 'error') {
      console.error('\n[bridge] Agent run failed:', result.id)
      process.exit(2)
    }
    console.log('\n[bridge] execution complete')
  }
}

main().catch(err => {
  console.error('[bridge] fatal:', err.message)
  process.exit(1)
})
