package migratewave

import (
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

func projectWaveStatus(spineIndex, done, ready int, streamStatus string) string {
	status := strings.ToLower(strings.TrimSpace(streamStatus))
	isClosed := status == "closed" || status == "signed"
	if spineIndex < done {
		return "done"
	}
	if spineIndex < done+ready {
		return "ready_for_signoff"
	}
	if isClosed {
		return "done"
	}
	if spineIndex == done+ready && status == "in_progress" {
		return "next"
	}
	return "pending"
}

func deriveStreamNextTask(streamID string, done, ready int, streamStatus string) string {
	waves, ok := wavesForStream(streamID)
	if !ok {
		return ""
	}
	switch streamID {
	case TradeK8sNativeStreamID:
		return deriveNextTaskFromWaves(waves, done, ready, streamStatus, true)
	case DataLayerK3sStreamID:
		return deriveNextTaskFromWaves(waves, done, ready, streamStatus, false)
	default:
		return deriveNextTaskFromWaves(waves, done, ready, streamStatus, false)
	}
}

func deriveNextTaskFromWaves(waves []Wave, done, ready int, streamStatus string, wStyleSigned bool) string {
	readyWaves := make([]Wave, 0)
	var nextWave *Wave
	for i := range waves {
		w := waves[i]
		switch projectWaveStatus(w.SpineIndex, done, ready, streamStatus) {
		case "ready_for_signoff":
			readyWaves = append(readyWaves, w)
		case "next":
			if nextWave == nil {
				nextWave = &w
			}
		}
	}
	if len(readyWaves) > 0 {
		ids := readyWaves[0].Code
		if len(readyWaves) > 1 {
			parts := make([]string, len(readyWaves))
			for i, w := range readyWaves {
				parts[i] = w.Code
			}
			ids = strings.Join(parts, "/")
		}
		tail := ""
		if nextWave != nil {
			tail = fmt.Sprintf(" → %s NEXT — %s", nextWave.Code, nextWave.Label)
		}
		return fmt.Sprintf("%s DELIVERED awaiting sign-off%s", ids, tail)
	}
	if nextWave != nil {
		suffix := signedSuffixForWaves(waves, done, wStyleSigned)
		if suffix != "" {
			return fmt.Sprintf("%s NEXT — %s. %s", nextWave.Code, nextWave.Label, suffix)
		}
		return fmt.Sprintf("%s NEXT — %s", nextWave.Code, nextWave.Label)
	}
	if strings.ToLower(streamStatus) == "closed" {
		return fmt.Sprintf("CLOSED — %s", signedSuffixForWaves(waves, done, wStyleSigned))
	}
	return ""
}

func signedSuffixForWaves(waves []Wave, done int, wRange bool) string {
	if done <= 0 || done > len(waves) {
		return ""
	}
	if wRange && done == 1 {
		return fmt.Sprintf("(%s signed)", waves[0].Code)
	}
	if wRange && done > 1 {
		return fmt.Sprintf("(%s–%s signed)", waves[0].Code, waves[done-1].Code)
	}
	if done == 1 {
		return fmt.Sprintf("(%s complete ✓)", waves[0].Code)
	}
	return fmt.Sprintf("(%s–%s complete ✓)", waves[0].Code, waves[done-1].Code)
}

// deriveFocusHeadline mirrors console/src/lib/briefing/reconcileBriefing.ts deriveFocusHeadline (D-D).
func deriveFocusHeadline(ctx *opscontext.File) string {
	if ctx == nil || ctx.Tracks == nil || ctx.Tracks.Migrate == nil {
		return ""
	}
	parts := []string{}

	for i := range ctx.Tracks.Migrate.Streams {
		s := &ctx.Tracks.Migrate.Streams[i]
		if s.ID != TradeK8sNativeStreamID {
			continue
		}
		status := strings.ToLower(s.Status)
		if status == "closed" {
			if nt := deriveStreamNextTask(TradeK8sNativeStreamID, s.Done, s.ReadyForSignoff, s.Status); nt != "" {
				parts = append(parts, fmt.Sprintf("Trade K8s-native %s", nt))
			} else {
				parts = append(parts, "Trade K8s-native CLOSED")
			}
			break
		}
		ready := s.ReadyForSignoff
		readyWaves := make([]Wave, 0)
		var nextWave *Wave
		for _, w := range tradeK8sNativeWaves {
			switch projectWaveStatus(w.SpineIndex, s.Done, ready, s.Status) {
			case "ready_for_signoff":
				readyWaves = append(readyWaves, w)
			case "next":
				if nextWave == nil {
					wCopy := w
					nextWave = &wCopy
				}
			}
		}
		if len(readyWaves) > 0 {
			ids := readyWaves[0].Code
			if len(readyWaves) > 1 {
				ws := make([]string, len(readyWaves))
				for j, w := range readyWaves {
					ws[j] = w.Code
				}
				ids = strings.Join(ws, "/")
			}
			tail := ""
			if nextWave != nil {
				tail = fmt.Sprintf(" → %s %s", nextWave.Code, nextWave.Label)
			}
			parts = append(parts, fmt.Sprintf("Trade K8s-native %s DELIVERED awaiting sign-off%s", ids, tail))
		} else if nextWave != nil {
			parts = append(parts, fmt.Sprintf("Trade K8s-native %s NEXT — %s", nextWave.Code, nextWave.Label))
		}
		break
	}

	for i := range ctx.Tracks.Migrate.Streams {
		s := &ctx.Tracks.Migrate.Streams[i]
		if s.ID != DataLayerK3sStreamID || strings.ToLower(s.Status) != "in_progress" {
			continue
		}
		if nt := deriveStreamNextTask(DataLayerK3sStreamID, s.Done, s.ReadyForSignoff, s.Status); nt != "" {
			parts = append(parts, fmt.Sprintf("data-layer: %s", nt))
		} else if s.NextTask != nil && *s.NextTask != "" {
			parts = append(parts, fmt.Sprintf("data-layer: %s", *s.NextTask))
		}
		break
	}
	return strings.Join(parts, " · ")
}

// Backward-compatible alias for tests.
func deriveTradeK8sNextTask(done, ready int, streamStatus string) string {
	return deriveStreamNextTask(TradeK8sNativeStreamID, done, ready, streamStatus)
}
