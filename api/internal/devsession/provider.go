package devsession

import "context"

// SessionProvider abstracts local bdev sessions vs cluster Deployments.
type SessionProvider interface {
	List(ctx context.Context) ([]DevSession, error)
	Logs(ctx context.Context, name string, lines int) (*LogResponse, error)
	Control(ctx context.Context, name, action string) (*ControlResponse, error)
}

const (
	ModeBdev = "bdev"
	ModeK8s  = "k8s"
)
