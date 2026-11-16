package store

import ()

// The presence pass (the controller's agentrt-presence loop, every five
// seconds; presence.go has the view). A host the controller reports offline
// (HostPresence.ReportedOffline) gets a policy_alert on its presence stream
// (HostPresenceStream; rule host_offline, its artifact saying when it last
// polled and what was running on it); once it polls again, a note there that
// it is back online. Only active hosts under no stop or drain are watched,
// both ways: an alert of a host taken out of work since is closed by a note
// and not told, and nothing is told of it while it is out (review 526 B1).
// An executor that is known not to be available gets a policy_alert on its
// own presence stream (ExecutorPresenceStream; rule executor_unavailable,
// its artifact saying why); once it is available again, a note there. Each
// turn is recorded once: the last record of each presence stream is its
// state (openAlerts), so a pass after a restart says nothing it said before.
// Each pass that records a turn it tells also tells the Owner in his
// conversation with the chief of staff, as the platform and not the seat
// (TellWaitingBriefs' precedent, briefwait.go): the chief of staff runs on
// the hosts it would report, so only the platform can say it within the
// minute. The note names the jobs no executor can take now (chief_of_staff
// among them: the chief of staff cannot answer) and those not known of; it
// says every job is covered again only when each has an executor known to be
// available (review 526 A2). The records and the note are one transaction,
// written under the seat's lock (taken first, as AcceptBrief and
// TellWaitingBriefs take it), and the view and the records are read again
// under it, so two passes never record a turn twice.

// The alerts' phrases, and the notes' words after the host or executor.
const (
	hostOfflinePhrase         = "a host stopped polling"
	executorUnavailablePhrase = "an executor cannot take work now"
)
