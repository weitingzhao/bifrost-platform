#!/usr/bin/env bash
# WiFiman baseline checklist — run BEFORE UniFi AP rollout (Migration step 1).
# Install WiFiman (iOS/Android), connect to current Eero WiFi, record dBm at each point.
set -euo pipefail

cat <<'EOF'
# WiFiman Baseline — Eero (pre-AP)

Target: ≥ -65 dBm on 5GHz at every point (catalog target).

| # | Location              | Floor | 2.4GHz dBm | 5GHz dBm | Notes        |
|---|-----------------------|-------|------------|----------|--------------|
| 1 | Office / Mac desk     |       |            |          | wired backup |
| 2 | Living room           | 1F    |            |          |              |
| 3 | Kitchen               | 1F    |            |          |              |
| 4 | Master bedroom        | 2F    |            |          |              |
| 5 | Kids room             | 2F/3F |            |          |              |
| 6 | 3F study              | 3F    |            |          |              |
| 7 | Basement (B1)         | B1    |            |          | mesh weak?   |
| 8 | Garage                |       |            |          | Ring cam     |
| 9 | Ring camera spot      |       |            |          | outdoor      |

Save screenshot export from WiFiman optional.
Re-run same table after AP rollout (Migration step 9) and compare.
EOF
