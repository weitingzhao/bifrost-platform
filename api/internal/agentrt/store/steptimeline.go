package store

import (
	"slices"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// StepTimelineEvent is a stored runtime event as the timeline reads it:
// the step's plan events, its tasks' events, and deploy_seen.
type StepTimelineEvent struct {
	EventID   int64
	Type      string
	At        time.Time
	Actor     string
	PlanNode  string
	TaskID    string
	AttemptID string
	Body      map[string]any
}

// StepLandingEntry is a landing entry with the id of the note that carried it.
type StepLandingEntry struct {
	EventID int64
	landing.Entry
}

// StepExternalRecord is one recorded version of an external work note.
type StepExternalRecord struct {
	EventID int64
	At      time.Time
	Note    externalNote
}

// StepTimelineInput is what FoldStepTimeline folds, already read: it never
// queries. Plan holds the step's plan events, Tasks its tasks' events,
// Landing the entries of those tasks' operations and of the STG operations
// that may carry them, Deploys the deploy_seen events, External the
// versions of the step's external work, and ExternalHosts the hosts whose
// kind is external (HostExternal).
type StepTimelineInput struct {
	Step          string
	Plan          []StepTimelineEvent
	Tasks         []StepTimelineEvent
	Landing       []StepLandingEntry
	Deploys       []StepTimelineEvent
	External      []StepExternalRecord
	ExternalHosts []string
}

// StepEvent is one move of a step, in the plan view's names.
type StepEvent struct {
	Kind       string `json:"kind"`
	At         string `json:"at"`
	Reason     string `json:"reason"`
	Source     string `json:"source"`
	EventID    int64  `json:"event_id"`
	PlanNodeID string `json:"plan_node_id"`
	TaskID     string `json:"task_id"`
	AttemptID  string `json:"attempt_id"`
	Actor      string `json:"actor"`
	WaitedOn   string `json:"waited_on"`
	WaitS      int64  `json:"wait_s"`

	at time.Time
}

// Sources of a step event.
const (
	stepSourceEvent       = "agentrt.event"
	stepSourceLanding     = "note.landing"
	stepSourceDeploy      = "deploy_seen"
	stepSourceExternal    = "external_work"
	stepSourceCoordinator = "coordinator_work"
)

// stepWaitAfter names what a step waits for after an event of a kind.
var stepWaitAfter = map[string]string{
	"created":          "Owner",
	"active":           "executor lease",
	"task_leased":      "executor",
	"coordinator_work": "executor",
	"external_work":    "executor",
	"submitted":        "review",
	"accepted":         "landing CI",
	"landed":           "STG",
	"on_stg":           "PROD release",
}

// FoldStepTimeline orders a step's events by time, then event id, each with
// the seconds since the step's previous event and what it waited on then.
// A stage with no record is absent; none is invented.
func FoldStepTimeline(in StepTimelineInput) []StepEvent {
	out := []StepEvent{}
	add := func(e StepEvent) {
		if e.PlanNodeID == "" {
			e.PlanNodeID = in.Step
		}
		out = append(out, e)
	}
	fromEvent := func(kind, source, reason string, e StepTimelineEvent) StepEvent {
		return StepEvent{Kind: kind, Source: source, Reason: reason, EventID: e.EventID, PlanNodeID: e.PlanNode,
			TaskID: e.TaskID, AttemptID: e.AttemptID, Actor: e.Actor, at: e.At}
	}

	for _, e := range in.Plan {
		switch e.Type {
		case "plan_node_created":
			add(fromEvent("created", stepSourceEvent, "", e))
		case "plan_node_state_changed":
			to, _ := e.Body["to"].(string)
			cause, _ := e.Body["cause"].(string)
			switch {
			case to == "active":
				add(fromEvent("active", stepSourceEvent, cause, e))
			case to == "done" && (cause == "done_rule" || cause == "done_rule_manual"):
				add(fromEvent("done_by_rule", stepSourceEvent, cause, e))
			}
		}
	}

	// An attempt on the external host is the coordinator's own work.
	coordinator := map[string]bool{}
	for _, e := range in.Tasks {
		switch e.Type {
		case "attempt_leased":
			host, _ := e.Body["host_id"].(string)
			executor, _ := e.Body["executor"].(string)
			x := fromEvent("task_leased", stepSourceEvent, "", e)
			x.Actor = executor
			if host != "" && slices.Contains(in.ExternalHosts, host) {
				coordinator[e.AttemptID] = true
				x.Kind, x.Source, x.Reason = "coordinator_work", stepSourceCoordinator, "leased"
			}
			add(x)
		case "attempt_submitted":
			if coordinator[e.AttemptID] {
				add(fromEvent("coordinator_work", stepSourceCoordinator, "submitted", e))
			} else {
				add(fromEvent("submitted", stepSourceEvent, "", e))
			}
		case "task_state_changed":
			if to, _ := e.Body["to"].(string); to == "accepted" {
				reason, _ := e.Body["reason"].(string)
				add(fromEvent("accepted", stepSourceEvent, reason, e))
			}
		}
	}

	addLanding := func(kind, reason string, l StepLandingEntry, task, attempt string) {
		add(StepEvent{Kind: kind, Source: stepSourceLanding, Reason: reason, EventID: l.EventID, TaskID: task, AttemptID: attempt, at: l.At})
	}
	// An operation's task and attempt are named by its claim.
	opTask, opAttempt := map[string]string{}, map[string]string{}
	pending := map[string][]string{}
	for _, l := range in.Landing {
		op := l.Record.String("op")
		switch l.Record.String("kind") {
		case "claimed":
			opTask[op], opAttempt[op] = l.Record.String("task"), l.Record.String("attempt")
		case "stg_pending":
			rs, _ := l.Record["R"].([]any)
			for _, r := range rs {
				if s, ok := r.(string); ok {
					pending[op] = append(pending[op], s)
				}
			}
		}
	}
	landed := map[string]bool{}
	for _, l := range in.Landing {
		if l.Record.String("kind") == "landed" {
			op := l.Record.String("op")
			landed[l.Record.String("R")] = true
			addLanding("landed", l.Record.String("R"), l, opTask[op], opAttempt[op])
		}
	}
	// The step is on STG when an STG operation carrying a landed R of it is
	// verified; that P, or one verified later, carries it to PROD.
	verified := slices.Clone(in.Landing)
	sort.SliceStable(verified, func(i, j int) bool { return entryBefore(verified[i], verified[j]) })
	prodP := map[string]bool{}
	var onSTG *StepLandingEntry
	for i, l := range verified {
		if l.Record.String("kind") != "stg_verified" {
			continue
		}
		carries := slices.ContainsFunc(pending[l.Record.String("op")], func(r string) bool { return landed[r] })
		if onSTG == nil && carries {
			onSTG = &verified[i]
		}
		if onSTG != nil {
			prodP[l.Record.String("P")] = true
		}
	}
	if onSTG != nil {
		addLanding("on_stg", onSTG.Record.String("P"), *onSTG, "", "")
	}
	deploys := slices.Clone(in.Deploys)
	sort.SliceStable(deploys, func(i, j int) bool {
		return deploys[i].At.Before(deploys[j].At) || deploys[i].At.Equal(deploys[j].At) && deploys[i].EventID < deploys[j].EventID
	})
	for _, e := range deploys {
		env, _ := e.Body["env"].(string)
		commit, _ := e.Body["commit"].(string)
		if e.Type == "deploy_seen" && env == "prod" && onSTG != nil && prodP[commit] && !onSTG.At.After(e.At) {
			x := fromEvent("on_prod", stepSourceDeploy, commit, e)
			x.PlanNodeID, x.TaskID, x.AttemptID = "", "", ""
			add(x)
			break
		}
	}

	// An external work's record joins the timeline when its state changes.
	last := map[string]string{}
	external := slices.Clone(in.External)
	sort.SliceStable(external, func(i, j int) bool {
		a, b := external[i], external[j]
		return a.At.Before(b.At) || a.At.Equal(b.At) && a.EventID < b.EventID
	})
	for _, r := range external {
		if s, ok := last[r.Note.ID]; ok && s == r.Note.State {
			continue
		}
		last[r.Note.ID] = r.Note.State
		add(StepEvent{Kind: "external_work", Source: stepSourceExternal, Reason: r.Note.State, EventID: r.EventID,
			PlanNodeID: r.Note.Node, TaskID: r.Note.Task, Actor: r.Note.Actor, at: r.At})
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.at.Before(b.at) || a.at.Equal(b.at) && a.EventID < b.EventID
	})
	for i := range out {
		out[i].At = timeText(out[i].at)
		if i > 0 {
			out[i].WaitS = max(0, int64(out[i].at.Sub(out[i-1].at).Seconds()))
			out[i].WaitedOn = stepWaitAfter[out[i-1].Kind]
		}
	}
	return out
}

func entryBefore(a, b StepLandingEntry) bool {
	return a.At.Before(b.At) || a.At.Equal(b.At) && a.EventID < b.EventID
}
