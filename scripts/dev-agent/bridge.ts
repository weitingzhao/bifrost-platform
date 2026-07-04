/**
 * Cursor SDK Bridge — called by Platform API to orchestrate dev agent tasks.
 *
 * Usage:
 *   node bridge.js --prompt "Execute Phase TIBM4..." --phase TIBM4
 *   node bridge.js --resume <agent-id> --prompt "Fix X"
 *
 * Requires: CURSOR_API_KEY env var
 */

import { Agent } from '@cursor/sdk'
import { parseArgs } from 'node:util'

const WORKSPACE_CWD = '/Users/vision-mac-trader/Desktop/stocks'

const { values } = parseArgs({
  options: {
    prompt: { type: 'string' },
    phase: { type: 'string' },
    resume: { type: 'string' },
  },
})

const apiKey = process.env.CURSOR_API_KEY
if (!apiKey) {
  console.error('[bridge] CURSOR_API_KEY not set')
  process.exit(1)
}

const prompt = values.prompt
if (!prompt) {
  console.error('[bridge] --prompt is required')
  process.exit(1)
}

async function main() {
  const resumeId = values.resume

  if (resumeId) {
    // Resume an existing agent conversation
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
    // Create a new agent session
    await using agent = await Agent.create({
      apiKey,
      model: { id: 'composer-2.5' },
      local: { cwd: WORKSPACE_CWD },
    })

    // Output agent ID for future resume
    console.log(`[bridge] agent_id=${agent.agentId}`)
    console.log(`[bridge] phase=${values.phase ?? 'unknown'}`)
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
