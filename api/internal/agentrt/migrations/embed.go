// Package migrations holds the agentrt schema as numbered SQL files, applied
// in order by package migrate. A file never changes once applied anywhere:
// the migrator compares sha256 and refuses an edited file.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
