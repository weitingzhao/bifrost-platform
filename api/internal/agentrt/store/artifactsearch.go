package store

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Searching the artifacts (W-31 2.3, search_artifacts; the Owner 2026-10-21
// 「2.3 的四条都照推荐 A」, CUT-round2-2.3-rest §3 第 1 条): their metadata,
// newest first, never a body (GET /artifacts/{id} reads one). A listing, not
// a feed: an artifact committed after a page was read, earlier than that
// page's last, is not listed on the next; every artifact in order is the
// artifact_put events'.
//
// An artifact is content-addressed and may serve many records, and its own
// task_id and attempt_id columns are not written, so a task's artifacts are
// the ones its records name: its spec, its attempts' reports, the evidence
// of its verifications, the messages on it, and every artifact an event of
// it names (artifact_id, text_artifact_id, artifacts, evidence). A
// mission's are its tasks', its spec, and those of the messages and events
// on it.

// ArtifactSearchLimit bounds a page; ArtifactSearchDefault is a page's size
// when none is asked.
const (
	ArtifactSearchLimit   = 100
	ArtifactSearchDefault = 50
)

// ArtifactQuery is a search: each field given narrows it.
type ArtifactQuery struct {
	Kinds     []string  // artifact kinds, any of them
	Task      string    // the artifacts the task's records name
	Mission   string    // the artifacts the mission's records name
	Author    string    // the actor that first stored it
	Since     time.Time // stored at or after
	Before    string    // a cursor from a previous page
	Limit     int       // 1 to ArtifactSearchLimit; 0 is ArtifactSearchDefault
	HideExams bool      // an Agent host's agentd: no exam artifact (GET /artifacts/{id}'s rule)
}

// ArtifactMeta is an artifact's metadata.
type ArtifactMeta struct {
	ID         string    `json:"artifact_id"`
	Kind       string    `json:"artifact_kind"`
	MediaType  string    `json:"media_type"`
	Size       int64     `json:"size_bytes"`
	Supersedes *string   `json:"supersedes_artifact_id"`
	CreatedBy  string    `json:"created_by_actor_id"`
	CreatedAt  time.Time `json:"created_at"`
}

var (
	missionIDRE = regexp.MustCompile(`^M-[1-9][0-9]*$`)
	taskIDRE    = regexp.MustCompile(`^t_` + ulid + `$`)
	cursorRE    = regexp.MustCompile(`^([0-9]{1,19})\.([0-9a-f]{64})$`)
)

// artifactCursor is a page's last artifact as the next page starts after it:
// its time in microseconds since the epoch and its digest.
func artifactCursor(m ArtifactMeta) string {
	return strconv.FormatInt(m.CreatedAt.UnixMicro(), 10) + "." + strings.TrimPrefix(m.ID, "sha256:")
}

func parseArtifactCursor(s string) (time.Time, string, error) {
	g := cursorRE.FindStringSubmatch(s)
	if g == nil {
		return time.Time{}, "", invalid("before is a cursor from a previous page (<microseconds>.<64 hex>), not %q", s)
	}
	us, err := strconv.ParseInt(g[1], 10, 64)
	if err != nil {
		return time.Time{}, "", invalid("before is a cursor from a previous page, not %q", s)
	}
	return time.UnixMicro(us), "sha256:" + g[2], nil
}

// check refuses a query that cannot be answered as asked.
func (q *ArtifactQuery) check() error {
	for _, k := range q.Kinds {
		if !ArtifactKinds[k] {
			return invalid("kind %q is not an artifact kind (%s)", k, strings.Join(keysOf(ArtifactKinds), ", "))
		}
	}
	switch {
	case q.Task != "" && !taskIDRE.MatchString(q.Task):
		return invalid("task is a task id (t_…), not %q", q.Task)
	case q.Mission != "" && !missionIDRE.MatchString(q.Mission):
		return invalid("mission is a mission id (M-n), not %q", q.Mission)
	case q.Author != "" && kindOf(q.Author) == "":
		return invalid("author is an actor's name, not %q", q.Author)
	case q.Limit < 0 || q.Limit > ArtifactSearchLimit:
		return invalid("limit is 1 to %d", ArtifactSearchLimit)
	}
	if q.Limit == 0 {
		q.Limit = ArtifactSearchDefault
	}
	return nil
}

// SearchArtifacts answers q: at most q.Limit artifacts, newest first, and the
// cursor of the next page ("" when this one is the last).
func SearchArtifacts(ctx context.Context, db DB, q ArtifactQuery) ([]ArtifactMeta, string, error) {
	if err := q.check(); err != nil {
		return nil, "", err
	}
	var beforeAt any
	beforeID := ""
	if q.Before != "" {
		at, id, err := parseArtifactCursor(q.Before)
		if err != nil {
			return nil, "", err
		}
		beforeAt, beforeID = at, id
	}
	var since any
	if !q.Since.IsZero() {
		since = q.Since
	}
	kinds := q.Kinds
	if kinds == nil {
		kinds = []string{}
	}
	var out []ArtifactMeta
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, artifactSearch, kinds, q.Task, q.Mission, q.Author, since, beforeAt, beforeID, q.HideExams,
			ExamsStream, q.Limit+1)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ArtifactMeta, error) {
			var m ArtifactMeta
			serr := r.Scan(&m.ID, &m.Kind, &m.MediaType, &m.Size, &m.Supersedes, &m.CreatedBy, &m.CreatedAt)
			m.CreatedAt = m.CreatedAt.UTC()
			return m, serr
		})
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("store: search artifacts: %w", err)
	}
	next := ""
	if len(out) > q.Limit {
		out = out[:q.Limit]
		next = artifactCursor(out[len(out)-1])
	}
	if out == nil {
		out = []ArtifactMeta{}
	}
	return out, next, nil
}

// artifactSearch: $1 the kinds (none: any), $2 the task, $3 the mission, $4
// the author, $5 since, $6 and $7 the cursor's time and digest, $8 whether
// exam artifacts are hidden, $9 the exams' stream, $10 the page and one more.
// The cursor's time is in microseconds, as created_at is stored.
const artifactSearch = `WITH tasks AS (
	    SELECT t.task_id FROM agentrt.task t
	    WHERE ($2 <> '' OR $3 <> '') AND ($2 = '' OR t.task_id = $2) AND ($3 = '' OR t.mission_id = $3)),
	  named AS (
	    SELECT t.spec_artifact_id AS id FROM agentrt.task t JOIN tasks USING (task_id)
	    UNION SELECT a.report_artifact_id FROM agentrt.attempt a JOIN tasks USING (task_id) WHERE a.report_artifact_id IS NOT NULL
	    UNION SELECT v.evidence_artifact_id FROM agentrt.verification v JOIN tasks USING (task_id)
	    UNION SELECT m.body_artifact_id FROM agentrt.message m
	      WHERE m.task_id IN (SELECT task_id FROM tasks) OR ($2 = '' AND $3 <> '' AND m.mission_id = $3)
	    UNION SELECT s.spec_artifact_id FROM agentrt.mission s WHERE $2 = '' AND $3 <> '' AND s.mission_id = $3 AND s.spec_artifact_id IS NOT NULL
	    UNION SELECT n.id FROM agentrt.event e
	      CROSS JOIN LATERAL (
	        SELECT e.body_json->>'artifact_id' AS id WHERE jsonb_typeof(e.body_json->'artifact_id') = 'string'
	        UNION ALL SELECT e.body_json->>'text_artifact_id' WHERE jsonb_typeof(e.body_json->'text_artifact_id') = 'string'
	        UNION ALL SELECT x FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(e.body_json->'artifacts') = 'array'
	          THEN e.body_json->'artifacts' ELSE '[]'::jsonb END) x
	        UNION ALL SELECT x FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(e.body_json->'evidence') = 'array'
	          THEN e.body_json->'evidence' ELSE '[]'::jsonb END) x) n
	      WHERE e.task_id IN (SELECT task_id FROM tasks) OR ($2 = '' AND $3 <> '' AND e.mission_id = $3))
	SELECT a.artifact_id, a.artifact_kind, a.media_type, a.size_bytes, a.supersedes_artifact_id, a.created_by_actor_id, a.created_at
	FROM agentrt.artifact a
	WHERE (cardinality($1::text[]) = 0 OR a.artifact_kind = ANY($1))
	  AND (($2 = '' AND $3 = '') OR a.artifact_id IN (SELECT id FROM named))
	  AND ($4 = '' OR a.created_by_actor_id = $4)
	  AND ($5::timestamptz IS NULL OR a.created_at >= $5)
	  AND ($6::timestamptz IS NULL OR (a.created_at, a.artifact_id) < ($6, $7::text))
	  AND NOT ($8 AND (a.artifact_kind = 'exam' OR EXISTS (SELECT 1 FROM agentrt.event x
	    WHERE x.event_stream_id = $9 AND x.body_json->>'artifact_id' = a.artifact_id)))
	ORDER BY a.created_at DESC, a.artifact_id DESC
	LIMIT $10`
