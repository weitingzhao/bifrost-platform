package agentgovernance

import "time"

const (
	PromotionThreshold = 3
)

type TrustMatrixEntry struct {
	SkillID              string `json:"skill_id"`
	SkillLabel           string `json:"skill_label"`
	CurrentLevel         string `json:"current_level"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	PromotionEligible    bool   `json:"promotion_eligible"`
	DemotionTriggered    bool   `json:"demotion_triggered"`
	LastOverrideAt       string `json:"last_override_at,omitempty"`
	LastOverrideBy       string `json:"last_override_by,omitempty"`
	SuggestedLevel       string `json:"suggested_level,omitempty"`
	SuggestedLevelReason string `json:"suggested_level_reason,omitempty"`
}

type TrustMatrixResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Entries     []TrustMatrixEntry `json:"entries"`
	DataSource  string             `json:"data_source"`
	// OverrideStore is the trust-overrides file path.
	OverrideStore string `json:"override_store,omitempty"`
	// StoreError is set when that file is missing or unreadable. The matrix
	// is still returned, without overrides.
	StoreError string `json:"store_error,omitempty"`
}
