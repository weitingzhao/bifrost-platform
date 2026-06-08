package probe

import "time"

type Reachability string

const (
	ReachOK       Reachability = "ok"
	ReachDegraded Reachability = "degraded"
	ReachFail     Reachability = "fail"
	ReachUnknown  Reachability = "unknown"
)

type AuthStatus string

const (
	AuthOK       AuthStatus = "ok"
	AuthMissing  AuthStatus = "missing"
	AuthInvalid  AuthStatus = "invalid"
	AuthSkipped  AuthStatus = "skipped"
	AuthBlocked  AuthStatus = "blocked"
)

type Principal struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

type Target struct {
	ID                 string       `json:"id"`
	Category           string       `json:"category"`
	Reachability       Reachability `json:"reachability"`
	Auth               AuthStatus   `json:"auth"`
	AuthorizationLevel string       `json:"authorization_level"`
	Detail             string       `json:"detail"`
	URL                string       `json:"url,omitempty"`
}

type MatrixResponse struct {
	Environment string    `json:"environment"`
	Label       string    `json:"label"`
	GeneratedAt time.Time `json:"generated_at"`
	Principal   Principal `json:"principal"`
	Targets     []Target  `json:"targets"`
}

type HTTPEndpoint struct {
	ID       string
	Category string
	Path     string
}
