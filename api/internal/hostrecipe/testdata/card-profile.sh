#!/usr/bin/env bash
# THIS IS THE NODE'S ROOT: the command below runs in the host namespaces of gpu-server (hostPID + nsenter) as root.
# It can read and change anything on that node, including its disks and the node's own credentials. The command text is the control: read it.
# Trade / IB pods live on gpu-server when this card was generated: none.
# D10 is not lifted: this card does not place orders, change a trading service, or touch those Pods, their volumes or their Secrets.
# owner-exec node-run v1 tier=host node=gpu-server runtime_sha256=0cbca2ff7d674afc53ec05c0ed59fbf9a32d0f910f731262b38a34a1ee86e9a8 recipe=gpu-strata-profile class=reversible recipe_sha256=8977a95febc13346941524e2c779434d22a931ea391c529089bd40bd1ac743e3
TIER=host
NODE=gpu-server
DEADLINE=300
IMG='192.168.10.73:30500/bifrost-research@sha256:8cd0b34bbff05e1c6bc21b555e9d196813c484f6d49514583a78def32b4b83c5'
read -r -d '' HOSTCMD <<'HOSTCMD_EOF'
MODEL=q4
CTX=131072
# recipe gpu-strata-profile (host tier, gpu-server only, REVERSIBLE): pick the model and the context.
# Parameters (set above this text by node-run.sh, whitelisted there): MODEL in {iq3, q4}, CTX in {32768, 65536, 131072, 262144}.
# What it changes: --max-context in that model's launch profile, and the systemd drop-in that makes the Strata
# service start the Q4 profile (present for q4, absent for iq3). Before touching anything it copies both into
# a timestamped backup; if Strata does not serve the asked context within about 4 minutes, it puts the backup
# back and restarts, and says so. Undo by hand: run this recipe again with the previous MODEL and CTX.
# No data, no credentials, no network rule is touched.
set -eu
[ "$(hostname -s)" = ubt-ai-4090 ]
L=/home/vision/.local/state/strata/launch
DROP=/etc/systemd/system/bifrost-strata-iq3.service.d
case "$MODEL" in iq3) P=$L/iq3_s-32k.json;; q4) P=$L/ud-q4_k_xl-32k.json;; *) echo "bad MODEL"; exit 2;; esac
case "$CTX" in 32768|65536|131072|262144) ;; *) echo "bad CTX"; exit 2;; esac
B=/var/lib/bifrost-strata-backups/$(date -u +%Y%m%dT%H%M%SZ); mkdir -p "$B"
cp -p $L/iq3_s-32k.json $L/ud-q4_k_xl-32k.json "$B/"; [ -d "$DROP" ] && cp -pr "$DROP" "$B/dropin" || true
echo "backup: $B"
python3 - "$P" "$CTX" <<'PY'
import json,sys
p,ctx=sys.argv[1],sys.argv[2]; j=json.load(open(p)); n=0
for k in ("args","args_written"):
    a=j.get(k)
    if isinstance(a,list) and "--max-context" in a:
        a[a.index("--max-context")+1]=ctx; n+=1
assert n>=1, "no --max-context"
j["port"]=8082
json.dump(j,open(p,"w"),indent=2); print("profile", p.rsplit("/",1)[1], "ctx", ctx)
PY
chown vision:vision "$P"
if [ "$MODEL" = q4 ]; then
  cur=$(systemctl show -p ExecStart --value bifrost-strata-iq3.service | sed -n 's/.*argv\[\]=\([^;]*\);.*/\1/p' | head -1 | sed 's/launch-ud-q4_k_xl-32k/launch-iq3_s-32k/')
  mkdir -p "$DROP"; printf '[Service]\nExecStart=\nExecStart=%s\n' "$(printf '%s' "$cur" | sed 's/launch-iq3_s-32k/launch-ud-q4_k_xl-32k/')" > "$DROP/q4-ab-test.conf"
else
  rm -f "$DROP/q4-ab-test.conf"
fi
systemctl daemon-reload
systemctl restart --no-block bifrost-strata-iq3.service
ok=0
for i in $(seq 1 46); do
  sleep 5
  c=$(curl -s -m 3 http://127.0.0.1:8082/props 2>/dev/null | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("n_ctx",""))
except Exception: print("")' || true)
  [ "$c" = "$CTX" ] && { ok=1; echo "serving $MODEL ctx $CTX after ~$((i*5))s"; break; }
done
if [ "$ok" = 1 ]; then echo RECIPE=gpu-strata-profile OK; exit 0; fi
echo "not serving $CTX after ~230s: restoring $B and restarting"
cp -p "$B/iq3_s-32k.json" "$B/ud-q4_k_xl-32k.json" $L/
rm -rf "$DROP"; [ -d "$B/dropin" ] && cp -pr "$B/dropin" "$DROP" || true
systemctl daemon-reload; systemctl restart --no-block bifrost-strata-iq3.service
echo RECIPE=gpu-strata-profile ROLLED_BACK; exit 1
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
