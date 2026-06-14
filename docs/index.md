# Bifrost Platform

AI-native **environment governance** control plane — separate from the trade monitoring UI.

## Related handbook

Deployment, hardware roadmap, Goal, and migration sign-off live in the **infra handbook**:

**[http://127.0.0.1:8050/](http://127.0.0.1:8050/)** — run `./scripts/start_docs.sh` in `bifrost-trade-infra`

## This site

| Topic | Document |
|-------|----------|
| **North star (ultimate goal)** | [NORTH_STAR.md](NORTH_STAR.md) |
| **MkDocs site** | `make docs` → http://127.0.0.1:8060/ (also linked from Console → Docs) |
| Repo overview | [Overview](overview.md) |
| Control plane vs data plane | [Architecture](ARCHITECTURE.md) |
| L0 probe contract | [Trade contract](TRADE_CONTRACT.md) |
| Node agent (Phase B) | [Agent](agent/README.md) |
| MCP tools (Phase P2) | [MCP](mcp/README.md) |

## Run the console

```bash
make start    # platform-api :8780 + console :5180
```
