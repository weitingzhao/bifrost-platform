package migratewave

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

type ActuationResponse struct {
	OK          bool                      `json:"ok"`
	Action      string                    `json:"action"`
	Target      string                    `json:"target"`
	Changed     bool                      `json:"changed"`
	Message     string                    `json:"message"`
	Stream      opscontext.MigrateStream  `json:"stream"`
	Headline    string                    `json:"headline"`
	GeneratedAt time.Time                 `json:"generated_at"`
}
