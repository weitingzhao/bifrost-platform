package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// An implementation proposed through the routes runs only on a spec agentd
// can check (R5:5.7-8): agentd reads a spec that is not a JSON object as text
// alone (agentd's ParseSpec), so its executor starts without the
// repository's setup and its deterministic check fails with "the spec names
// no verify command", every round (PROD M-308, 2026-11-05). The tasks a plan
// produces have their spec from ImplementSpec and are not checked here; nor
// are a pinned task's, which is the platform's own (pin.go).

// agentdSpec is agentd's TaskSpec (agentd/spec.go), field for field: agentd
// imports this package, so its parser cannot be called from here. A spec with
// a field agentd does not know is one agentd refuses.
type agentdSpec struct {
	Text     string     `json:"text"`
	Verify   [][]string `json:"verify,omitempty"`
	TimeoutS int        `json:"timeout_s,omitempty"`
	Setup    [][]string `json:"setup,omitempty"`
	Siblings []struct {
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
	} `json:"siblings,omitempty"`
	Check        string `json:"check,omitempty"`
	SubjectTask  string `json:"subject_task,omitempty"`
	Attempt      string `json:"attempt,omitempty"`
	Round        int    `json:"round,omitempty"`
	SubjectSpec  string `json:"task_spec,omitempty"`
	Commit       string `json:"commit,omitempty"`
	Repo         string `json:"repo,omitempty"`
	Verification string `json:"verification,omitempty"`
	VerifyReport string `json:"verify_report,omitempty"`
	Salvage      bool   `json:"salvage,omitempty"`
}

// implementSpecLacks says what an implementation's spec in repo lacks for
// agentd to set it up and check it, verify or setup ("" for nothing), and
// why: it is agentd's JSON task
// spec with a verify command, and with a setup list when Repos declares one
// for the repository.
func implementSpecLacks(repo string, body []byte) (what, why string) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "verify", "it is not agentd's JSON task spec, which agentd would read as text with no checks"
	}
	var s agentdSpec
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return "verify", "it is not agentd's JSON task spec (" + err.Error() + ")"
	}
	if len(s.Verify) == 0 {
		return "verify", "it names no verify command"
	}
	for _, cmds := range [][][]string{s.Verify, s.Setup} {
		for _, c := range cmds {
			if len(c) == 0 || c[0] == "" {
				return "verify", "it has an empty command, which agentd refuses"
			}
		}
	}
	if len(Repos[repo].Setup) > 0 && len(s.Setup) == 0 {
		return "setup", "it has no setup, which " + repo + " needs before an executor starts"
	}
	return "", ""
}

// checkImplementSpecs refuses a batch with an implementation on a branch whose
// spec implementSpecLacks something, naming the task's ref and what is
// missing. pins are the batch's pinned refs, whose spec is the platform's. A
// spec not stored is left to checkSpecArtifact.
func checkImplementSpecs(ctx context.Context, tx pgx.Tx, tasks []TaskProposal, pins map[string]string) error {
	for _, t := range tasks {
		if t.Role != "implement" || t.Effect != "branch" || pins[t.Ref] != "" {
			continue
		}
		var body []byte
		err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, t.SpecArtifact).Scan(&body)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if what, why := implementSpecLacks(t.Repo, body); what != "" {
			return invalid("task %s: spec %s lacks %s (%s); an implement spec is the JSON task spec ImplementSpec builds, with verify and setup",
				t.Ref, t.SpecArtifact, what, why)
		}
	}
	return nil
}
