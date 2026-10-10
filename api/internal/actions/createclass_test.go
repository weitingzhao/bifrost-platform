package actions

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassifyCreateKeepsOnlyDefiniteRefusals(t *testing.T) {
	gr := schema.GroupResource{Group: "tekton.dev", Resource: "pipelineruns"}
	definite := []error{
		apierrors.NewBadRequest("no"),
		apierrors.NewForbidden(gr, "run", errors.New("no")),
		apierrors.NewNotFound(gr, "run"),
		apierrors.NewConflict(gr, "run", errors.New("version")),
		&apierrors.StatusError{ErrStatus: metav1.Status{Code: http.StatusUnprocessableEntity, Message: "invalid"}},
	}
	for _, err := range definite {
		got := ClassifyCreate(err)
		if IsUncertain(got) || got != err {
			t.Fatalf("%v became %v; a definite refusal must stay a failure", err, got)
		}
	}

	uncertain := []error{
		io.ErrUnexpectedEOF,
		io.EOF,
		fmt.Errorf("read: %w", syscall.ECONNRESET),
		apierrors.NewTimeoutError("timeout", 1),
		apierrors.NewAlreadyExists(gr, "run"),
		apierrors.NewInternalError(errors.New("boom")),
		apierrors.NewServiceUnavailable("down"),
		apierrors.NewTooManyRequests("slow", 1),
		apierrors.NewUnauthorized("no"),
		errors.New("unclassified"),
	}
	for _, err := range uncertain {
		got := ClassifyCreate(err)
		if !IsUncertain(got) {
			t.Fatalf("%v stayed a definite refusal (%v); an unclassified create must be unknown", err, got)
		}
		if !strings.Contains(got.Error(), "create outcome unknown") {
			t.Fatalf("%v message = %q", err, got.Error())
		}
	}
}
