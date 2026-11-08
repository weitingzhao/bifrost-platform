package landing

// FloorFile is one file M0..R changes, as Git has it at R: its change kind
// and modes, the lowercase sha256 of its bytes and whether it declares a
// Secret (rules.InfraSecretManifest). A deletion has no bytes.
type FloorFile struct {
	Path, Kind, OldMode, Mode, SHA256 string
	Secret                            bool
}

// FloorTree is what a floor grant is checked against, recomputed from Git:
// whether R is one commit whose only parent is M0, and every file M0..R
// changes.
type FloorTree struct {
	OnM0  bool
	Files []FloorFile
}
