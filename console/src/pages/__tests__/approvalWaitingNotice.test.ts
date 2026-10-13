import { expect, it } from 'vitest'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'

it('says closing the page does not cancel a decision the server has received', () => {
  expect(APPROVAL_WAITING_NOTICE).toBe(
    'Still waiting for the server. Closing this page does not cancel a decision the server has already received; reopen this page to check that it was recorded. This can take up to 5 minutes.',
  )
})
