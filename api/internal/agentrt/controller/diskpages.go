package controller

import (
	"time"
)

// DiskPagesEvery is how often the controller pages the Owner of hosts' disk
// records (TD-415): a page the relay did not take is tried again a minute
// later, a relay away for longer is a loop failing (loopalert.go).
const DiskPagesEvery = time.Minute
