// Package mustrefuse holds the approval service's must-refuse set: requests
// that must be refused, or must stay cards, however the service is changed
// (Owner 2026-11-15: no second reader; the author proves a change with machine
// facts, and the set that defines "must refuse" is the Owner's to change).
// The definitions and the tests that run them live in this directory only, so
// one path pattern in the signed policy's owner_only group protects them all.
// The tests are an external package and use the service through its public
// surface (the routes Mount registers), as a caller does.
package mustrefuse
