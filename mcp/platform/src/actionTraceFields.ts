import { z } from 'zod'

// Keep caller wording verbatim; whitespace-only answers are not evidence.
const material = (description: string) => z.string().refine(value => value.trim().length > 0, description).describe(description)
export const actionTraceFields = {
  reason: material('reason is required: explain why this action is needed'),
  unchecked: material('unchecked is required: list checks not performed; say none if all were performed'),
  rollback: material('rollback is required: explain how to revert'),
}
