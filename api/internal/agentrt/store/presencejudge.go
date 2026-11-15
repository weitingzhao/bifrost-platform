package store

import (
	"time"
)

// When host presence cannot be judged, or its judgment is deferred (the
// coordinating thread on reviews 528 and 530: a probe that fails, a long
// outage, or an outage the probe keeps seeing every few passes is never
// silent). Each is a switch with a stream of its own: a policy_alert when it
// turns on, a note when it turns off, each told once to the Owner in his
// conversation with the chief of staff. The stream holds nothing else: its
// head is the state, read the bounded way (headSQL). GET /progress says each
// (presence_unjudged, presence_deferred), and the Console with them.
//
// Unjudged: the controller judges hosts only while the platform takes
// agentd's calls (its probe says so, or some host claimed within
// HostLostAfter); while it cannot tell for longer than a while, nothing is
// judged.
//
// Deferred: the hosts are judged, but a host's silence counts from no
// earlier than the platform's last recovery, and when the probe keeps seeing
// outages every few passes (with several API instances its sampling of the
// Service is probabilistic: it may land on instances that are not ready
// while another serves), each recovery restarts the hosts' 30 s; a host
// silent for longer than PresenceDeferredAfter that is still not judged lost
// for that reason makes the judgment deferred.

// PresenceStream holds whether host presence can be judged; DeferredStream
// whether its judgment is deferred.
const (
	PresenceStream = "presence:judging"
	DeferredStream = "presence:deferred"
)

// PresenceDeferredAfter is how long a host may be silent, not judged lost
// because the platform's recoveries keep moving its floor, before the
// judgment is said to be deferred.
const PresenceDeferredAfter = 2 * time.Minute

// The alerts' phrases.
const (
	presenceUnjudgedPhrase = "host presence cannot be judged"
	presenceDeferredPhrase = "host presence judgment is deferred"
)
