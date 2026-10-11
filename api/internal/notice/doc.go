// Package notice cleans text and posts one message to the alert relay.
//
// The cleaners are separate functions and keep the outputs they had when
// they lived next to approvals, notifications and progress documents.
// Nothing in this package imports actions or approvals, so a later caller
// can clean text and send a notice without taking those dependencies.
//
// PushField is how one agent-written field becomes push text. Redact is
// unchanged. Send refuses a title or body that still contains a hidden
// character instead of rewriting it.
//
// Environment (either unset → a "skipped" delivery, no HTTP):
//
//	APPROVAL_NOTIFY_URL    full URL of POST /api/v1/alerts/notify
//	APPROVAL_NOTIFY_TOKEN  bearer equal to the relay's ALERT_RELAY_TOKEN
package notice
