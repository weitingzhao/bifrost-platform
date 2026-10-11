// Package notice cleans text and posts one message to the alert relay.
// Send replaces a title or body that contains a hidden character with the
// caller's server-written fallback. It does not change the composed text
// of a message that is already clean.
//
// The cleaners are separate functions and keep the outputs they had when
// they lived next to approvals, notifications and progress documents.
// Nothing in this package imports actions or approvals, so a later caller
// can clean text and send a notice without taking those dependencies.
//
// Environment (either unset → a "skipped" delivery, no HTTP):
//
//	APPROVAL_NOTIFY_URL    full URL of POST /api/v1/alerts/notify
//	APPROVAL_NOTIFY_TOKEN  bearer equal to the relay's ALERT_RELAY_TOKEN
package notice
