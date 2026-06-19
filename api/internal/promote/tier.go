package promote

// GateTier separates STG release gate from Prod cutover gate.
type GateTier string

const (
	GateTierStg  GateTier = "stg"
	GateTierProd GateTier = "prod"
)

func ParseGateTier(raw string) GateTier {
	switch raw {
	case string(GateTierStg):
		return GateTierStg
	default:
		return GateTierProd
	}
}
