package probe

import (
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// MissionSignal mirrors Console missionSignals Signal (matrix-derived payload status).
type MissionSignal string

const (
	MissionOK       MissionSignal = "ok"
	MissionDegraded MissionSignal = "degraded"
	MissionFail     MissionSignal = "fail"
	MissionUnknown  MissionSignal = "unknown"
)

// TradeEnvSnapshot is matrix-derived trade/payload status for one environment.
type TradeEnvSnapshot struct {
	Environment string        `json:"environment"`
	Label       string        `json:"label"`
	Signal      MissionSignal `json:"signal"`
	Reachable   int           `json:"reachable"`
	Total       int           `json:"total"`
	Detail      string        `json:"detail"`
}

// PostFixVerification is the autonomous-loop verdict after remediation.
type PostFixVerification struct {
	Passed                        bool   `json:"passed"`
	MissionMatrixNominal          bool   `json:"mission_matrix_nominal"`
	DatastoreVerificationNominal  bool   `json:"datastore_verification_nominal"`
	ProbeDriftRemaining           bool   `json:"probe_drift_remaining"`
	Detail                        string `json:"detail"`
	AgentGuidance                 string `json:"agent_guidance"`
}

// VerifyMissionSnapshotResponse is returned by GET /api/v1/mission/verify-snapshot.
type VerifyMissionSnapshotResponse struct {
	GeneratedAt         time.Time              `json:"generated_at"`
	TradeDev            TradeEnvSnapshot       `json:"trade_dev"`
	TradeProd           TradeEnvSnapshot       `json:"trade_prod"`
	PayloadOverall      MissionSignal          `json:"payload_overall"`
	PayloadVerification VerifyPayloadResponse  `json:"payload_verification"`
	PostFixVerification PostFixVerification    `json:"post_fix_verification"`
}

func tradeEnvSnapshot(env config.Environment, matrix MatrixResponse) TradeEnvSnapshot {
	targets := make([]Target, 0, len(matrix.Targets))
	for _, t := range matrix.Targets {
		if strings.HasPrefix(t.Category, "trade") || t.Category == "datastore" {
			targets = append(targets, t)
		}
	}
	total := len(targets)
	if total == 0 {
		return TradeEnvSnapshot{
			Environment: env.ID,
			Label:       env.Label,
			Signal:      MissionUnknown,
			Total:       0,
			Detail:      "no trade/datastore targets probed",
		}
	}
	up := 0
	anyFail := false
	anyDeg := false
	for _, t := range targets {
		switch t.Reachability {
		case ReachOK:
			up++
		case ReachFail:
			anyFail = true
		case ReachDegraded:
			anyDeg = true
		}
	}
	sig := MissionDegraded
	switch {
	case anyFail:
		sig = MissionFail
	case anyDeg:
		sig = MissionDegraded
	case up == total:
		sig = MissionOK
	}
	return TradeEnvSnapshot{
		Environment: env.ID,
		Label:       env.Label,
		Signal:      sig,
		Reachable:   up,
		Total:       total,
		Detail:      fmt.Sprintf("%d/%d services reachable", up, total),
	}
}

func worstMissionSignal(signals ...MissionSignal) MissionSignal {
	order := []MissionSignal{MissionFail, MissionDegraded, MissionUnknown, MissionOK}
	for _, want := range order {
		for _, s := range signals {
			if s == want {
				return want
			}
		}
	}
	return MissionUnknown
}

func buildPostFixVerification(payloadOverall MissionSignal, payload VerifyPayloadResponse) PostFixVerification {
	matrixNominal := payloadOverall == MissionOK
	datastoreNominal := payload.Summary.Overall == ClassNominal && payload.Summary.DataLayerCount == 0
	probeDrift := payload.Summary.ProbeDriftCount > 0
	passed := matrixNominal && datastoreNominal && !probeDrift

	var detailParts []string
	if matrixNominal {
		detailParts = append(detailParts, "payload matrix NOMINAL")
	} else {
		detailParts = append(detailParts, fmt.Sprintf("payload matrix %s", payloadOverall))
	}
	detailParts = append(detailParts, fmt.Sprintf("verify_payload overall=%s", payload.Summary.Overall))
	if probeDrift {
		detailParts = append(detailParts, fmt.Sprintf("probe_drift=%d", payload.Summary.ProbeDriftCount))
	}

	guidance := "Post-fix verification passed — safe to close remediation job."
	if !passed {
		guidance = "Post-fix verification NOT passed — re-probe with verify_mission_snapshot before closing. " +
			"If PROBE_DRIFT remains, fix platform probe (L2); if DATA_LAYER, continue datastore remediation (L1). " +
			"Do not declare success until post_fix_verification.passed is true."
	}

	return PostFixVerification{
		Passed:                       passed,
		MissionMatrixNominal:         matrixNominal,
		DatastoreVerificationNominal: datastoreNominal,
		ProbeDriftRemaining:          probeDrift,
		Detail:                       strings.Join(detailParts, " · "),
		AgentGuidance:                guidance,
	}
}

// VerifyMissionSnapshot fresh-probes matrix + cluster and returns post-fix verification verdict.
func VerifyMissionSnapshot(
	envs []config.Environment,
	matrices []MatrixResponse,
	ds DatastoreSnapshot,
) VerifyMissionSnapshotResponse {
	now := time.Now().UTC()
	matrixByEnv := map[string]MatrixResponse{}
	for _, m := range matrices {
		matrixByEnv[m.Environment] = m
	}

	payloadVerify := VerifyPayload(envs, matrices, ds)

	var devSnap, prodSnap TradeEnvSnapshot
	for _, env := range envs {
		matrix, ok := matrixByEnv[env.ID]
		if !ok {
			continue
		}
		snap := tradeEnvSnapshot(env, matrix)
		switch env.ID {
		case "dev":
			devSnap = snap
		case "prod":
			prodSnap = snap
		}
	}

	payloadOverall := worstMissionSignal(devSnap.Signal, prodSnap.Signal)

	return VerifyMissionSnapshotResponse{
		GeneratedAt:         now,
		TradeDev:            devSnap,
		TradeProd:           prodSnap,
		PayloadOverall:      payloadOverall,
		PayloadVerification: payloadVerify,
		PostFixVerification: buildPostFixVerification(payloadOverall, payloadVerify),
	}
}
