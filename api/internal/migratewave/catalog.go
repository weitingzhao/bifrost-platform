package migratewave

// Multi-stream migrate wave catalogs — loaded from config/migrate-waves/*.yaml.

const (
	TradeK8sNativeStreamID = "trade-k8s-native"
	DataLayerK3sStreamID   = "data-layer-k3s"
)

type Wave struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	SpineIndex int    `json:"spine_index"`
	Label      string `json:"label"`
	Repo       string `json:"repo,omitempty"`
	Verify     string `json:"verify,omitempty"`
	BlockedBy  string `json:"blocked_by,omitempty"`
	Delivered  string `json:"delivered,omitempty"`
	Goal       string `json:"goal,omitempty"`
}

// streamWaves populated by InitMigrateWaveCatalog.
var streamWaves = map[string][]Wave{}

func wavesForStream(streamID string) ([]Wave, bool) {
	_ = ensureMigrateCatalog("")
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	w, ok := streamWaves[streamID]
	return w, ok
}

func waveByID(streamID, id string) (*Wave, bool) {
	waves, ok := wavesForStream(streamID)
	if !ok {
		return nil, false
	}
	for i := range waves {
		if waves[i].ID == id {
			return &waves[i], true
		}
	}
	return nil, false
}

func waveBySpineIndex(streamID string, idx int) (*Wave, bool) {
	waves, ok := wavesForStream(streamID)
	if !ok {
		return nil, false
	}
	for i := range waves {
		if waves[i].SpineIndex == idx {
			return &waves[i], true
		}
	}
	return nil, false
}
