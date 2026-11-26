#!/usr/bin/env bash
# THIS IS THE NODE'S ROOT: the command below runs in the host namespaces of gpu-server (hostPID + nsenter) as root.
# It can read and change anything on that node, including its disks and the node's own credentials. The command text is the control: read it.
# Trade / IB pods live on gpu-server when this card was generated: none.
# D10 is not lifted: this card does not place orders, change a trading service, or touch those Pods, their volumes or their Secrets.
# owner-exec node-run v1 tier=host node=gpu-server runtime_sha256=0cbca2ff7d674afc53ec05c0ed59fbf9a32d0f910f731262b38a34a1ee86e9a8 recipe=gpu-models class=read-only recipe_sha256=42774c3aed1cbb96a33041cb05692df146f3a2cc2b19f21e2e76ae265ed29598
TIER=host
NODE=gpu-server
DEADLINE=300
IMG='192.168.10.73:30500/bifrost-research@sha256:8cd0b34bbff05e1c6bc21b555e9d196813c484f6d49514583a78def32b4b83c5'
read -r -d '' HOSTCMD <<'HOSTCMD_EOF'
# recipe gpu-models (host tier, gpu-server only, READ-ONLY): model files on disk, launch profiles, disk room.
set -eu
[ "$(hostname -s)" = ubt-ai-4090 ]
M=/home/vision/gpu-lab/strata-20261101/models
find $M -maxdepth 3 -name '*.gguf' -printf '%s %p\n' 2>/dev/null | awk '{printf "%.1fG %s\n",$1/1e9,$2}' | sed "s|$M/||" | head -20
ls /home/vision/.local/state/strata/launch/ | grep -E '\.json$' | grep -v choices
df -h $M | awk 'NR==2{print "disk:", $3, "used of", $2, "free", $4}'
echo RECIPE=gpu-models DONE
HOSTCMD_EOF
NS=bifrost-owner-exec
RUN="node-run-$(date -u +%Y%m%d-%H%M%S)"   # lowercase only: a Pod name is an RFC 1123 label (card #601 died on "T" and "Z")
NAME="$RUN-$(echo "$NODE" | tr -c 'a-z0-9' '-' | sed 's/-*$//')"
[[ "$NAME" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] && [ "${#NAME}" -le 63 ] || { echo "RESULT=FAIL stage=name detail=$NAME is not a valid Pod name"; exit 1; }
cleanup() { kubectl delete pod "$NAME" -n "$NS" --wait=false >/dev/null 2>&1 || true; }
trap cleanup EXIT
die() { echo "RESULT=FAIL stage=$1 detail=$2"; exit 1; }

# 1. the node must exist and be Ready (a node that is off is refused: wake it first)
ready=$(kubectl get node "$NODE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)
[ "$ready" = True ] || die node-not-ready "$NODE ready=${ready:-missing}"

# 2. host tier only: print which Trade / IB workloads are live on the node right now (information, not a refusal: the Owner decided
#    on 2026-11-18 that the host tier may be used on such nodes, one card at a time, each card read and approved by him).
if [ "$TIER" = host ]; then
  live=$(kubectl get pods -A --field-selector "spec.nodeName=$NODE,status.phase!=Succeeded,status.phase!=Failed" --no-headers -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name 2>/dev/null \
        | awk '$1=="bifrost-prod"||$1=="bifrost-stg"||$1=="bifrost-dev"||$2 ~ /^redis-ib/||$2 ~ /^ib-gateway/||$2 ~ /^daemon/ {print $1"/"$2}' | paste -sd, -)
  echo "live Trade/IB pods on $NODE at run time: ${live:-none}"
fi

# 3. build the Pod. device: no host namespaces at all. host: hostPID + nsenter into PID 1's namespaces.
if [ "$TIER" = device ]; then
  CMD='["python3", "-I", "-c"]'; HOSTPID=false
  ARGS=$(printf '%s\n' "$RECIPE_SCRIPT" | sed 's/^/      /')
else
  CMD='["nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "/bin/sh", "-c"]'; HOSTPID=true
  ARGS=$(printf '%s\n' "$HOSTCMD" | sed 's/^/      /')
fi
cat <<EOF | kubectl apply -f - >/dev/null || die create-pod "$NAME"
apiVersion: v1
kind: Pod
metadata:
  name: $NAME
  namespace: $NS
  labels: {bifrost.io/purpose: node-run, bifrost.io/node-run-tier: $TIER}
spec:
  nodeName: $NODE
  restartPolicy: Never
  automountServiceAccountToken: false
  hostPID: $HOSTPID
  hostNetwork: false
  hostIPC: false
  activeDeadlineSeconds: ${DEADLINE:-300}
  containers:
  - name: run
    image: $IMG
    imagePullPolicy: IfNotPresent
    command: $CMD
    args:
    - |
$ARGS
    securityContext: {privileged: true, runAsUser: 0}
    resources:
      requests: {cpu: 50m, memory: 32Mi}
      limits: {cpu: "1", memory: 256Mi}
EOF
for _ in $(seq 1 ${WAIT_LOOPS:-150}); do
  phase=$(kubectl get pod "$NAME" -n "$NS" -o jsonpath='{.status.phase}' 2>/dev/null)
  if [ "$phase" = Succeeded ] || [ "$phase" = Failed ]; then break; fi
  sleep 2
done
echo "tier=$TIER node=$NODE phase=${phase:-unknown}"
kubectl logs "$NAME" -n "$NS" 2>&1 | tail -c 1700
[ "$phase" = Succeeded ] || die run "pod ended in phase ${phase:-unknown}"
