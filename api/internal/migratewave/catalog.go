package migratewave

// Multi-stream migrate wave catalogs — keep in sync with console architecture catalogs (D-C).

const (
	TradeK8sNativeStreamID = "trade-k8s-native"
	DataLayerK3sStreamID   = "data-layer-k3s"
)

type Wave struct {
	ID         string
	Code       string // W0..W11 or ①..⑦ display prefix
	SpineIndex int
	Label      string
}

var streamWaves = map[string][]Wave{
	TradeK8sNativeStreamID: tradeK8sNativeWaves,
	DataLayerK3sStreamID:   dataLayerK3sWaves,
}

func wavesForStream(streamID string) ([]Wave, bool) {
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
