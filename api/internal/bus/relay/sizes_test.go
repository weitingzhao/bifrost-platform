package relay

import "testing"

// 2026-11-16: on PROD the two EV streams reserved 5 GiB each, the ROCKET
// account's whole 10 GB, and the first receipt stream was refused with
// "insufficient storage resources available" (err_code 10047).
func TestStreamsFitTheAccount(t *testing.T) {
	envs := int64(2) // stg and prod share the account
	if got := envs * (EventStreamBytes + ReceiptStreamBytes); got > accountFileBytes {
		t.Fatalf("streams reserve %d bytes, the account holds %d", got, accountFileBytes)
	}
}
