# satellite-probe-bridge

Read-only HTTP bridge on the **developer Mac** — probes **K3s bifrost-dev** trade ingress (`:30882`) so `platform-api` can serve `bus-deep` for `dev-local` (Vision V1 thin-client reachability).

## Endpoints

| Path | Description |
|------|-------------|
| `GET /health` | Bridge liveness + configured probe base |
| `GET /bus-snapshot` | Probes `/api/monitor/status` and `/api/ops/ops/market-ingest/services` on K3s dev |

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `SATELLITE_PROBE_BRIDGE_PORT` | `8786` | Listen port |
| `TRADE_NGINX_BASE` | `http://192.168.10.73:30882` | K3s dev trade ingress (alias: `TRADE_DEV_BASE`) |
| `SATELLITE_PROBE_TIMEOUT_MS` | `8000` | Per-endpoint timeout |

Override `TRADE_NGINX_BASE` only for non-standard ingress (e.g. full local compose with nginx on `:80`).

## platform-api wiring

In `bifrost-platform/.env`:

```bash
SATELLITE_PROBE_BRIDGE_URL=http://127.0.0.1:8786
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

Requires LAN reachability to K3s dev ingress (`192.168.10.73:30882`). Does **not** require local `make dev` compose.
