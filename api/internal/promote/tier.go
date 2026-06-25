package promote

// GateTier separates release gate scopes.
// Trade gates: stg / prod.  Platform gates: platform-stg / platform-prod.
type GateTier string

const (
	GateTierStg          GateTier = "stg"
	GateTierProd         GateTier = "prod"
	GateTierPlatformStg  GateTier = "platform-stg"
	GateTierPlatformProd GateTier = "platform-prod"
)

func ParseGateTier(raw string) GateTier {
	switch raw {
	case string(GateTierStg):
		return GateTierStg
	case string(GateTierPlatformStg):
		return GateTierPlatformStg
	case string(GateTierPlatformProd):
		return GateTierPlatformProd
	default:
		return GateTierProd
	}
}

func IsPlatformTier(t GateTier) bool {
	return t == GateTierPlatformStg || t == GateTierPlatformProd
}
