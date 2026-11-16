package topologyview

// Adapter supplies deployment-specific display knowledge. It neither probes
// nor acts: the caller supplies all observations before Build runs.
type Adapter struct {
	TargetNodes func(targetID, environment string) []string
	EdgeDetail  func(kind string) (string, bool)
}

func (a Adapter) targetNodes(targetID, environment string) []string {
	if a.TargetNodes == nil {
		return nil
	}
	return a.TargetNodes(targetID, environment)
}

func (a Adapter) edgeDetail(kind string) (string, bool) {
	if a.EdgeDetail == nil {
		return "", false
	}
	return a.EdgeDetail(kind)
}
