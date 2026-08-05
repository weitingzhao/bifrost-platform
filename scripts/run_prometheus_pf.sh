#!/usr/bin/env bash
# bdev session: Prometheus port-forward with Bifrost k3s kubeconfig.
# Default: ~/.kube/bifrost-k3s.yaml (server should be the cluster VIP, not 127.0.0.1).
set -euo pipefail

export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/bifrost-k3s.yaml}"

if [[ ! -f "$KUBECONFIG" ]]; then
  echo "[prometheus-pf] KUBECONFIG not found: $KUBECONFIG" >&2
  echo "[prometheus-pf] Set KUBECONFIG or place bifrost-k3s.yaml under ~/.kube/" >&2
  exit 1
fi

# Avoid fake-healthy leftovers from a manual kubectl port-forward.
if command -v lsof >/dev/null 2>&1; then
  pids=$(lsof -iTCP:9090 -sTCP:LISTEN -t 2>/dev/null || true)
  if [[ -n "${pids:-}" ]]; then
    echo "[prometheus-pf] freeing :9090 (PIDs ${pids//$'\n'/ })"
    # shellcheck disable=SC2086
    kill -TERM $pids 2>/dev/null || true
    sleep 0.4
    pids=$(lsof -iTCP:9090 -sTCP:LISTEN -t 2>/dev/null || true)
    if [[ -n "${pids:-}" ]]; then
      # shellcheck disable=SC2086
      kill -KILL $pids 2>/dev/null || true
      sleep 0.2
    fi
  fi
fi

echo "[prometheus-pf] KUBECONFIG=$KUBECONFIG"
server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
echo "[prometheus-pf] cluster server=${server:-unknown}"
if [[ "$server" == *"127.0.0.1"* ]] || [[ "$server" == *"localhost"* ]]; then
  echo "[prometheus-pf] refusing localhost kube-apiserver — check KUBECONFIG ($KUBECONFIG)" >&2
  exit 1
fi

exec kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090
