package releasefreeze

import (
	"context"
	"errors"
	"net"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// Gate is a stateless reader. A value kept across calls and a value constructed
// for one call see the same cluster: the gate stores nothing it has observed.
type Gate struct{}

// Read performs one GET of ListPath. Transport failure, timeout and a cancelled
// context are unreadable. The request is not retried.
func (Gate) Read(ctx context.Context, c rest.Interface) Decision {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result := c.Get().AbsPath(ListPath).MaxRetries(0).Do(ctx)
	var code int
	result.StatusCode(&code)
	body, err := result.Raw()
	if code == 0 && err != nil {
		// A non-JSON error body makes client-go drop the HTTP status onto the
		// error and leave Result.statusCode at 0. The status is still the
		// server's answer; only a missing status is a transport failure.
		var status apierrors.APIStatus
		if errors.As(err, &status) {
			if recovered := int(status.Status().Code); recovered != 0 {
				return DecideHTTP(recovered, body)
			}
		}
		return DecideTransport(transportToken(err))
	}
	return DecideHTTP(code, body)
}

func transportToken(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "connection failed"
}
