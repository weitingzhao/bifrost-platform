package store

import ()

// The chief of staff answers its mail (W-31 2.3 slice 4; design v5 §1 row 8:
// in round 2 the seat is a task, made when the Owner speaks). A message to
// the seat (an ask, a note or a call for help) becomes a mission with one
// brief task, in one transaction that also takes the message off the seat's
// unread list and links it to the task: the controller's intake pass. A
// fresh session in the seat (the job chief_of_staff's candidates, read-only)
// reads the message, the conversation before it and what the runtime knows
// now, and hands in its answer as its report; the controller posts the report
// to the sender as an answer linked to the task, and accepts the task. A
// brief that ends without an answer (blocked, cancelled, or a report that is
// not there) gets an answer saying so, so a question is never left without
// one. The seat's memory is these records, not a conversation.

// BriefSuccess is a brief's mission's success criterion.
const BriefSuccess = "the chief of staff's answer is sent"

// BriefOffPlanReason is why a brief is off the plan tree.
const BriefOffPlanReason = "a message to the chief of staff's seat; the seat answers it in a brief"
