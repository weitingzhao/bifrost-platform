package migratewave

// dataLayerK3sWaves — sync with console/src/lib/architecture/dataLayerCatalog.ts DATA_LAYER_MIGRATION_PHASES

var dataLayerK3sWaves = []Wave{
	{ID: "data-0-cnpg-operator", Code: "①", SpineIndex: 0, Label: "Label ubt-k3s-02 postgres-role + deploy CloudNativePG operator + bifrost-postgres cluster (data NS)"},
	{ID: "data-1-minio-backup", Code: "②", SpineIndex: 1, Label: "MinIO backup target (nfs-hot) + CNPG barmanObjectStore WAL archive"},
	{ID: "data-2-stg-cutover", Code: "③", SpineIndex: 2, Label: "STG cutover — apps connect bifrost-postgres-rw.data.svc + redis-live/queue-stg; remove bifrost-stg in-ns postgres/redis"},
	{ID: "data-3-dev-cutover", Code: "④", SpineIndex: 3, Label: "DEV cutover — bifrost-dev config → data NS endpoints; remove bifrost-dev in-ns postgres/redis"},
	{ID: "data-4-prod-pg", Code: "⑤", SpineIndex: 4, Label: "PROD PG migrate — pg_dump legacy .80 → CNPG bifrost_prod; maintenance window + rollback plan"},
	{ID: "data-5-redis-split", Code: "⑥", SpineIndex: 5, Label: "PROD/STG redis-live + redis-queue split (Bitnami HA); Celery → redis-queue only"},
	{ID: "data-6-retire-embedded", Code: "⑦", SpineIndex: 6, Label: "Retire embedded stateful — remove postgres/redis from bifrost-* base; bare .80 PG standby or offline"},
}
