package planpace

import (
	_ "time/tzdata" // the Owner's zone, whatever the image carries
)

// The chief of staff speaks first (W-31 2.4 slice ④; Owner 2026-10-18
// 「简报 8:00 和 21:00 只发 Chief 页」). Twice a day, and when something is
// stuck, the controller leaves the seat a note; the intake pass makes it a
// brief like any message (brief.go), and the seat's answer to a note of the
// controller's goes to the Owner, as a note on his Chief page. A note's body
// names its slot, or the one item it is about, and a body is its artifact's
// id: the same note is never left twice, so each slot is briefed once and
// each item nudged once.

// BriefZone is the Owner's time zone; the briefs are at 8:00 and 21:00 there.
const BriefZone = "America/Chicago"
