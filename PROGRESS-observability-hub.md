# Progress — Observability Hub (Mission Control)

**Status**: ✅ Complete (2026-07-21)  
**Mode**: Ops · batch execution (Owner-approved)  
**D10**: unchanged — read-only monitoring only

## Goal

Aggregate existing monitoring into Mission Control → Observability, organized by Apollo Domain, answering “is the whole system healthy right now?” Grafana remains deep evidence.

## Waves

| Wave | Status | Summary |
|------|--------|---------|
| W0 Contract & ownership | ✅ | Signal registry, verdict rules, Grafana catalog, governance `observabilityCatalog.ts` |
| W1 One-screen hub | ✅ | `ObservabilityPage` — System Verdict, Domain Health, Attention, Selected Domain |
| W2 Telemetry wiring | ✅ | Console `fetchTelemetryAlerts` / `fetchTelemetryTargets`; Go presets for node + ib-gateway |
| W3 Converge entries | ✅ | Satellite Telemetry → Satellite Runtime; compact MonitoringCoverageStrip; Cluster → Observability link |
| W4 Grafana loop | ✅ | Dashboard catalog + safe URL builder + Attention triage sheet |

## Decision Log (honored)

- Verdict SSOT: `buildObservabilityViewModel` only — pages must not re-derive
- Required vs evidence; EXPECTED OFF neutral; Defects excluded
- Shared deps counted once; Mission Control / Governance → NOT OBSERVED when no contract
- Satellite Bus view model + K8s workloads evidence-only preserved
- No new npm/Go dependencies; no live trading / D10 unlock

## Verify

```bash
cd console && npm run test:observability && npm run test:satellite-bus && npm run type-check && npm run build
cd .. && make test
```
