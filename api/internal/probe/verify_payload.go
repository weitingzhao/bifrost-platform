package probe

import (
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// PayloadClassification is the per-env or per-component diagnosis bucket for Agent playbooks.
type PayloadClassification string

const (
	ClassNominal    PayloadClassification = "NOMINAL"
	ClassProbeDrift PayloadClassification = "PROBE_DRIFT"
	ClassDataLayer  PayloadClassification = "DATA_LAYER"
	ClassHTTPFail   PayloadClassification = "HTTP_FAIL"
	ClassUnknown    PayloadClassification = "UNKNOWN"
)

// DatastoreComponentView compares matrix vs cluster signals for one datastore target.
type DatastoreComponentView struct {
	MatrixReach  Reachability          `json:"matrix_reachability"`
	ClusterReach Reachability          `json:"cluster_reachability"`
	Class        PayloadClassification `json:"classification"`
	Detail       string                `json:"detail"`
}

// EnvPayloadVerification is the structured verify_payload result for one environment.
type EnvPayloadVerification struct {
	Environment   string                 `json:"environment"`
	Label         string                 `json:"label"`
	Classification PayloadClassification `json:"classification"`
	Postgres      DatastoreComponentView `json:"postgres"`
	Redis         DatastoreComponentView `json:"redis"`
	HTTPFailures  []string               `json:"http_failures"`
	Detail        string                 `json:"detail"`
}

// VerifyPayloadSummary rolls up counts across environments.
type VerifyPayloadSummary struct {
	Overall          PayloadClassification `json:"overall"`
	ProbeDriftCount  int                   `json:"probe_drift_count"`
	DataLayerCount   int                   `json:"data_layer_count"`
	HTTPFailCount    int                   `json:"http_fail_count"`
	NominalCount     int                   `json:"nominal_count"`
}

// VerifyPayloadResponse is returned by GET /api/v1/mission/verify-payload.
type VerifyPayloadResponse struct {
	GeneratedAt  time.Time                `json:"generated_at"`
	Environments []EnvPayloadVerification `json:"environments"`
	Summary      VerifyPayloadSummary     `json:"summary"`
}

func classifyDatastorePair(matrixReach, clusterReach Reachability, matrixDetail string) (PayloadClassification, string) {
	matrixDetailLower := strings.ToLower(matrixDetail)
	inClusterDNS := strings.Contains(matrixDetailLower, "svc.cluster.local") &&
		strings.Contains(matrixDetailLower, "lookup")

	if matrixReach == ReachOK && (clusterReach == ReachOK || clusterReach == ReachDegraded || clusterReach == ReachUnknown) {
		return ClassNominal, "matrix and cluster agree — healthy"
	}
	if matrixReach == ReachFail && clusterReach == ReachOK {
		return ClassProbeDrift, "matrix fail but cluster API ok — sensor/probe drift (platform defect, not payload outage)"
	}
	if matrixReach == ReachDegraded && clusterReach == ReachOK {
		return ClassProbeDrift, "matrix degraded but cluster API ok — likely probe path issue"
	}
	if inClusterDNS && clusterReach == ReachOK {
		return ClassProbeDrift, "in-cluster DNS probe from Mac host contradicted by cluster API"
	}
	if matrixReach == ReachFail || clusterReach == ReachFail {
		return ClassDataLayer, "matrix and/or cluster report datastore unavailable — real data layer issue"
	}
	if matrixReach == ReachDegraded || clusterReach == ReachDegraded {
		return ClassDataLayer, "datastore degraded on matrix or cluster"
	}
	return ClassUnknown, "insufficient signal to classify"
}

func worstClassification(classes ...PayloadClassification) PayloadClassification {
	order := []PayloadClassification{ClassDataLayer, ClassHTTPFail, ClassProbeDrift, ClassUnknown, ClassNominal}
	for _, c := range order {
		for _, x := range classes {
			if x == c {
				return c
			}
		}
	}
	return ClassUnknown
}

func reachFromSnapshot(row DatastoreEnvReach, kind string) Reachability {
	if kind == "postgres" {
		if row.Postgres != "" {
			return row.Postgres
		}
		return ReachUnknown
	}
	if row.Redis != "" {
		return row.Redis
	}
	return ReachUnknown
}

func buildEnvVerification(
	env config.Environment,
	matrix MatrixResponse,
	ds DatastoreSnapshot,
) EnvPayloadVerification {
	row, hasDS := ds.ByEnv[env.ID]
	pgMatrix := findTargetReach(matrix, "postgres")
	pgCluster := ReachUnknown
	if hasDS {
		pgCluster = reachFromSnapshot(row, "postgres")
	}
	pgClass, pgDetail := classifyDatastorePair(pgMatrix.reach, pgCluster, pgMatrix.detail)

	rdMatrix := findTargetReach(matrix, "redis")
	rdCluster := ReachUnknown
	if hasDS {
		rdCluster = reachFromSnapshot(row, "redis")
	}
	rdClass, rdDetail := classifyDatastorePair(rdMatrix.reach, rdCluster, rdMatrix.detail)

	var httpFails []string
	for _, t := range matrix.Targets {
		if t.Category != "trade_api" && t.Category != "trade_frontend" {
			continue
		}
		if t.Reachability == ReachFail {
			httpFails = append(httpFails, t.ID)
		}
	}

	httpClass := ClassNominal
	if len(httpFails) > 0 {
		httpClass = ClassHTTPFail
	}

	overall := worstClassification(pgClass, rdClass, httpClass)
	detail := summarizeEnvDetail(overall, pgClass, rdClass, httpFails)

	return EnvPayloadVerification{
		Environment:    env.ID,
		Label:          env.Label,
		Classification: overall,
		Postgres: DatastoreComponentView{
			MatrixReach: pgMatrix.reach, ClusterReach: pgCluster,
			Class: pgClass, Detail: pgDetail,
		},
		Redis: DatastoreComponentView{
			MatrixReach: rdMatrix.reach, ClusterReach: rdCluster,
			Class: rdClass, Detail: rdDetail,
		},
		HTTPFailures: httpFails,
		Detail:       detail,
	}
}

type targetReach struct {
	reach  Reachability
	detail string
}

func findTargetReach(matrix MatrixResponse, id string) targetReach {
	for _, t := range matrix.Targets {
		if t.ID == id {
			return targetReach{reach: t.Reachability, detail: t.Detail}
		}
	}
	return targetReach{reach: ReachUnknown, detail: "not probed"}
}

func summarizeEnvDetail(overall, pg, rd PayloadClassification, httpFails []string) string {
	parts := []string{string(overall)}
	if pg != ClassNominal {
		parts = append(parts, "postgres="+string(pg))
	}
	if rd != ClassNominal {
		parts = append(parts, "redis="+string(rd))
	}
	if len(httpFails) > 0 {
		parts = append(parts, "http_fail="+strings.Join(httpFails, ","))
	}
	return strings.Join(parts, " · ")
}

// VerifyPayload builds matrix + cluster comparison for all registered environments.
func VerifyPayload(
	envs []config.Environment,
	matrices []MatrixResponse,
	ds DatastoreSnapshot,
) VerifyPayloadResponse {
	now := time.Now().UTC()
	matrixByEnv := map[string]MatrixResponse{}
	for _, m := range matrices {
		matrixByEnv[m.Environment] = m
	}

	out := VerifyPayloadResponse{
		GeneratedAt:  now,
		Environments: make([]EnvPayloadVerification, 0, len(envs)),
	}

	for _, env := range envs {
		matrix, ok := matrixByEnv[env.ID]
		if !ok {
			continue
		}
		ev := buildEnvVerification(env, matrix, ds)
		out.Environments = append(out.Environments, ev)
		switch ev.Classification {
		case ClassNominal:
			out.Summary.NominalCount++
		case ClassProbeDrift:
			out.Summary.ProbeDriftCount++
		case ClassDataLayer:
			out.Summary.DataLayerCount++
		case ClassHTTPFail:
			out.Summary.HTTPFailCount++
		}
	}

	classes := make([]PayloadClassification, 0, len(out.Environments))
	for _, e := range out.Environments {
		classes = append(classes, e.Classification)
	}
	out.Summary.Overall = worstClassification(classes...)
	if len(classes) == 0 {
		out.Summary.Overall = ClassUnknown
	}

	return out
}
