package store

// ClaimFor is ClaimNext with a lease of the test's choosing, so a test can
// start from a lease that has lapsed, or is past its grace, without waiting.
var ClaimFor = claimNext
