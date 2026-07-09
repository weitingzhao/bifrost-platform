# satellite-probe-bridge

Read-only HTTP bridge on the **developer Mac** — probes local Trade compose nginx so remote `platform-api` can serve `bus-deep` for `dev-local` without LAN access to `127.0.0.1:80`.

## Endpoints

| Path | Description |
|------|-------------|
| `GET /health` | Bridge liveness + configured `trade_nginx_base` |
| `GET /bus-snapshot` | Probes `/api/monitor/status` and `/api/ops/ops/market-ingest/services` |

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `SATELLITE_PROBE_BRIDGE_PORT` | `8786` | Listen port |
| `TRADE_NGINX_BASE` | `http://127.0.0.1:80` | Local compose nginx |
| `SATELLITE_PROBE_TIMEOUT_MS` | `8000` | Per-endpoint timeout |

## platform-api wiring

In `bifrost-platform/.env`:

```bash
SATELLITE_PROBE_BRIDGE_URL=http://192.168.10.40:8786
```

`environments.yaml` registers `dev-local` with `probe_mode: bridge`. Restart platform-api after changing env.

## Run

```bash
cd agent/satellite-probe-bridge
npm install
./start.sh              # foreground
./start.sh daemon       # background (survives terminal close)
./start.sh status
./start.sh stop
```

Requires local Trade stack (`make dev` in bifrost-trade-infra) with nginx on port 80.
