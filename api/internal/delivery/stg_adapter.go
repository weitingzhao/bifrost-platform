package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	STGReleaseNamespace = "cicd"
	STGReleasePipeline  = "bifrost-deliver-platform"
	STGReleaseFinalizer = "bifrost.io/stg-release-hold"
	stgNamespace        = "bifrost-platform-stg"
	stgHealthURL        = "http://platform-api.bifrost-platform-stg.svc:8780/health"
)

var (
	ErrSTGEvidence   = errors.New("STG evidence does not match")
	ErrSTGCollision  = errors.New("STG release run identity collision")
	ErrSTGUnknown    = errors.New("STG evidence is unknown")
	ErrSTGWrongInput = fmt.Errorf("%w: wrong_input", ErrSTGEvidence)
	// ErrSTGCreateUnknown: a Create was sent and neither its response nor a
	// readback proves what the apiserver stored: both failed, or the object
	// they returned under the run's name does not match its identity.
	ErrSTGCreateUnknown = errors.New("create outcome unknown")
)

// STGReleaseAdapter supplies only external facts and named writes. It has no
// controller, credentials, database, window renewal or retry loop. Factories
// must return clients for the workers' identity; tests inject fake clusters.
type STGReleaseAdapter struct {
	Core    func() (kubernetes.Interface, error)
	Dynamic func() (dynamic.Interface, error)
	HTTP    *http.Client
	Now     func() time.Time
	Archive RunEvidenceArchive
}

func (a *STGReleaseAdapter) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}
func (a *STGReleaseAdapter) core() (kubernetes.Interface, error) {
	if a.Core == nil {
		return nil, ErrSTGUnknown
	}
	c, err := a.Core()
	if err == nil && c == nil {
		err = ErrSTGUnknown
	}
	return c, err
}
func (a *STGReleaseAdapter) dynamic() (dynamic.Interface, error) {
	if a.Dynamic == nil {
		return nil, ErrSTGUnknown
	}
	c, err := a.Dynamic()
	if err == nil && c == nil {
		err = ErrSTGUnknown
	}
	return c, err
}
func stgContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 10*time.Second)
}
