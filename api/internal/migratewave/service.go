package migratewave

import (
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

type Service struct {
	cfg *config.Config
}

func NewService(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) MarkDelivered(streamID, waveID, actor string) (ActuationResponse, error) {
	now := time.Now().UTC()
	waves, ok := wavesForStream(streamID)
	if !ok {
		return ActuationResponse{}, fmt.Errorf("stream %q: wave deliver not implemented", streamID)
	}
	_ = waves
	wave, ok := waveByID(streamID, waveID)
	if !ok {
		return ActuationResponse{}, fmt.Errorf("unknown wave id %q for stream %s", waveID, streamID)
	}

	ctx := s.loadSpine()
	stream, err := findStream(ctx, streamID)
	if err != nil {
		return ActuationResponse{}, err
	}
	if strings.ToLower(stream.Status) == "closed" {
		return ActuationResponse{}, fmt.Errorf("stream %s is closed", streamID)
	}

	ready := stream.ReadyForSignoff
	projected := projectWaveStatus(wave.SpineIndex, stream.Done, ready, stream.Status)
	if projected != "next" {
		return ActuationResponse{}, fmt.Errorf(
			"wave %s is %s — only the active NEXT wave (spineIndex %d) can be marked delivered",
			wave.Code, projected, stream.Done+ready,
		)
	}

	newReady := ready + 1
	if stream.Done+newReady > stream.Total {
		return ActuationResponse{}, fmt.Errorf("cannot deliver: done(%d)+ready(%d) would exceed total(%d)",
			stream.Done, newReady, stream.Total)
	}

	nextTask := deriveStreamNextTask(streamID, stream.Done, newReady, stream.Status)
	patchCtx := cloneContextWithStream(ctx, streamID, stream.Done, newReady, stream.Status, nextTask, stream.Note)
	headline := deriveFocusHeadline(patchCtx)

	if err := s.applyPatch(streamID, stream.Done, newReady, stream.Status, nextTask, stream.Note, headline); err != nil {
		return ActuationResponse{}, err
	}

	updated := s.streamAfterReload(streamID)
	return ActuationResponse{
		OK:          true,
		Action:      "migratewave.deliver",
		Target:      fmt.Sprintf("%s/%s", streamID, waveID),
		Changed:     true,
		Message:     fmt.Sprintf("%s marked DELIVERED by %s — awaiting Owner sign-off", wave.Code, actor),
		Stream:      updated,
		Headline:    headline,
		GeneratedAt: now,
	}, nil
}

func (s *Service) Signoff(streamID, waveID, notes, actor string) (ActuationResponse, error) {
	now := time.Now().UTC()
	if _, ok := wavesForStream(streamID); !ok {
		return ActuationResponse{}, fmt.Errorf("stream %q: wave sign-off not implemented", streamID)
	}
	wave, ok := waveByID(streamID, waveID)
	if !ok {
		return ActuationResponse{}, fmt.Errorf("unknown wave id %q for stream %s", waveID, streamID)
	}

	ctx := s.loadSpine()
	stream, err := findStream(ctx, streamID)
	if err != nil {
		return ActuationResponse{}, err
	}
	if strings.ToLower(stream.Status) == "closed" {
		return ActuationResponse{}, fmt.Errorf("stream %s is closed", streamID)
	}

	ready := stream.ReadyForSignoff
	if ready <= 0 {
		return ActuationResponse{}, fmt.Errorf("no waves awaiting sign-off on stream %s", streamID)
	}

	expected, _ := waveBySpineIndex(streamID, stream.Done)
	if expected == nil {
		return ActuationResponse{}, fmt.Errorf("no wave at spine index %d", stream.Done)
	}
	if wave.ID != expected.ID {
		return ActuationResponse{}, fmt.Errorf(
			"sign-off queue expects %s (%s) first — cannot sign %s out of order",
			expected.Code, expected.ID, wave.Code,
		)
	}

	newDone := stream.Done + 1
	newReady := ready - 1
	status := stream.Status
	if newDone >= stream.Total {
		status = "closed"
	}
	nextTask := deriveStreamNextTask(streamID, newDone, newReady, status)
	note := appendSignedNote(stream.Note, wave.Code, notes)
	patchCtx := cloneContextWithStream(ctx, streamID, newDone, newReady, status, nextTask, note)
	headline := deriveFocusHeadline(patchCtx)

	if err := s.applyPatch(streamID, newDone, newReady, status, nextTask, note, headline); err != nil {
		return ActuationResponse{}, err
	}

	updated := s.streamAfterReload(streamID)
	return ActuationResponse{
		OK:          true,
		Action:      "migratewave.signoff",
		Target:      fmt.Sprintf("%s/%s", streamID, waveID),
		Changed:     true,
		Message:     fmt.Sprintf("%s SIGNED by %s (%d/%d)", wave.Code, actor, newDone, stream.Total),
		Stream:      updated,
		Headline:    headline,
		GeneratedAt: now,
	}, nil
}

func appendSignedNote(existing, waveCode, notes string) string {
	stamp := fmt.Sprintf("%s ✓ signed", waveCode)
	if strings.Contains(existing, stamp) {
		return existing
	}
	trimmed := strings.TrimSpace(existing)
	if notes != "" {
		stamp = fmt.Sprintf("%s (%s)", stamp, strings.TrimSpace(notes))
	}
	if trimmed == "" {
		return stamp
	}
	return trimmed + " · " + stamp
}

func (s *Service) applyPatch(streamID string, done, ready int, status, nextTask, note, headline string) error {
	if s.cfg == nil || s.cfg.OpsContextPath == "" {
		return fmt.Errorf("ops context path not configured")
	}
	if err := opscontext.PatchMigrateStream(s.cfg.OpsContextPath, opscontext.MigrateStreamPatch{
		StreamID:        streamID,
		Done:            done,
		ReadyForSignoff: ready,
		Status:          status,
		NextTask:        nextTask,
		Note:            note,
		Headline:        headline,
	}); err != nil {
		return err
	}
	return s.cfg.ReloadOpsContext()
}

func (s *Service) streamAfterReload(streamID string) opscontext.MigrateStream {
	ctx := s.loadSpine()
	if stream, err := findStream(ctx, streamID); err == nil {
		return *stream
	}
	return opscontext.MigrateStream{ID: streamID}
}

func findStream(ctx *opscontext.File, streamID string) (*opscontext.MigrateStream, error) {
	if ctx == nil || ctx.Tracks == nil || ctx.Tracks.Migrate == nil {
		return nil, fmt.Errorf("migrate track not loaded")
	}
	for i := range ctx.Tracks.Migrate.Streams {
		if ctx.Tracks.Migrate.Streams[i].ID == streamID {
			return &ctx.Tracks.Migrate.Streams[i], nil
		}
	}
	return nil, fmt.Errorf("migrate stream %q not found", streamID)
}

func cloneContextWithStream(
	ctx *opscontext.File,
	streamID string,
	done, ready int,
	status, nextTask, note string,
) *opscontext.File {
	if ctx == nil {
		return nil
	}
	out := *ctx
	if ctx.Tracks != nil {
		tracks := *ctx.Tracks
		if ctx.Tracks.Migrate != nil {
			migrate := *ctx.Tracks.Migrate
			streams := make([]opscontext.MigrateStream, len(ctx.Tracks.Migrate.Streams))
			copy(streams, ctx.Tracks.Migrate.Streams)
			for i := range streams {
				if streams[i].ID == streamID {
					streams[i].Done = done
					streams[i].ReadyForSignoff = ready
					streams[i].Status = status
					nt := nextTask
					streams[i].NextTask = &nt
					streams[i].Note = note
				}
			}
			migrate.Streams = streams
			tracks.Migrate = &migrate
		}
		out.Tracks = &tracks
	}
	return &out
}

func (s *Service) loadSpine() *opscontext.File {
	if s.cfg == nil {
		return nil
	}
	if s.cfg.OpsContext != nil {
		return s.cfg.OpsContext
	}
	path := s.cfg.OpsContextPath
	if path == "" && s.cfg.ConfigPath != "" {
		path = opscontext.ResolvePath(s.cfg.ConfigPath)
	}
	ctx, err := opscontext.Load(path)
	if err != nil {
		return nil
	}
	return ctx
}
