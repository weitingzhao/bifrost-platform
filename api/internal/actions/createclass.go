package actions

import (
	"errors"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ClassifyCreate decides an error from the call that creates an object.
//
// A nil error stays nil. A status that proves nothing was created is returned
// unchanged, so the caller can keep it as a failure or, if it was already
// wrapped, a transient refusal. Those statuses are 400, 403, 404, 422, and
// 409 Conflict that is not AlreadyExists. AlreadyExists means the name is
// taken; it is not proof the object is absent.
//
// Timeouts, EOF, connection resets, every 5xx, and any error this list does
// not name are Uncertain. The approval goes to unknown. The original text is
// kept so a person can see why, and so secret redaction still has a marker
// to find.
func ClassifyCreate(err error) error {
	if err == nil || IsUncertain(err) || IsTransient(err) {
		return err
	}
	if definiteCreateRefusal(err) {
		return err
	}
	return Uncertain("create outcome unknown; outcome needs checking: " + err.Error())
}

// definiteCreateRefusal is an API status that proves the apiserver stored
// nothing. AlreadyExists is excluded: the object is there and has to be
// checked by hand.
func definiteCreateRefusal(err error) bool {
	if err == nil || apierrors.IsAlreadyExists(err) {
		return false
	}
	var status apierrors.APIStatus
	if !errors.As(err, &status) || status == nil {
		return false
	}
	switch status.Status().Code {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	case http.StatusConflict:
		return !apierrors.IsAlreadyExists(err)
	default:
		return false
	}
}
