package approvals

import (
	"os"
	"testing"
)

// Existing state-machine fixtures model the PROD approval home in isolated stores.
// Environment-boundary tests override this explicitly, including an unset identity.
func TestMain(m *testing.M) {
	os.Setenv("OPS_VIEWER_ENV", "prod")
	os.Exit(m.Run())
}
