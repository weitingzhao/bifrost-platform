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
