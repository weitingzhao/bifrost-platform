package controller

import (
	"time"
)

// Historical host-presence timing constants. The dispatch presence pass is
// retired; these names do not schedule a controller loop.
const PresenceEvery = 5 * time.Second
const PlatformProbeLimit = 3 * time.Second
const UnjudgedAfter = 2 * time.Minute
