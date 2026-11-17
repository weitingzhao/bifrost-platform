set -eu; kubectl apply -f - <<'TTL_CRONJOB_EOF'
apiVersion: batch/v1
kind: CronJob
metadata:
  name: tekton-pipelinerun-ttl
  namespace: cicd
  labels:
    bifrost.io/purpose: pipelinerun-ttl
spec:
  schedule: "20 * * * *"
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 1
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      ttlSecondsAfterFinished: 86400
      backoffLimit: 1
      template:
        spec:
          serviceAccountName: tekton-pipelinerun-ttl
          restartPolicy: OnFailure
          nodeSelector:
            kubernetes.io/arch: amd64
          containers:
            - name: prune
              image: docker.io/library/alpine:3.20
              imagePullPolicy: IfNotPresent
              env:
                - name: NAMESPACE
                  value: cicd
                - name: MAX_AGE_SECONDS
                  value: "172800"
                - name: POD_MAX_AGE_SECONDS
                  value: "3600"
                - name: FAILED_POD_MAX_AGE_SECONDS
                  value: "86400"
                - name: WORKSPACE_MAX_AGE_SECONDS
                  value: "86400"
                - name: KEEP_PER_TASK
                  value: "5"
              resources:
                requests:
                  cpu: 50m
                  memory: 64Mi
                limits:
                  cpu: 300m
                  # 2Gi since card #586 (2026-11-16), which raised it on the cluster to get the
                  # job running again before pass 1 was paged. Paged, the job fits the old
                  # 256Mi; the number stays what the cluster has until it is lowered there.
                  memory: 2Gi
              command:
                - /bin/sh
                - -c
                - |
                  set -eu
                  [ -n "${SKIP_APK:-}" ] || apk add --no-cache kubectl jq >/dev/null
                  NS="${NAMESPACE:-cicd}"
                  MAX_AGE_SECONDS="${MAX_AGE_SECONDS:-172800}"
                  POD_MAX_AGE_SECONDS="${POD_MAX_AGE_SECONDS:-3600}"
                  FAILED_POD_MAX_AGE_SECONDS="${FAILED_POD_MAX_AGE_SECONDS:-86400}"
                  WORKSPACE_MAX_AGE_SECONDS="${WORKSPACE_MAX_AGE_SECONDS:-86400}"
                  KEEP_PER_TASK="${KEEP_PER_TASK:-5}"
                  NOW="${NOW_EPOCH:-$(date -u +%s)}"
                  # DRY_RUN=1: same listing and counts, deletes sent with --dry-run=server.
                  DRY="${DRY_RUN:+--dry-run=server}"
                  work="$(mktemp -d)"
                  trap 'rm -rf "${work}"' EXIT
                  # Pass 1: terminal PipelineRuns older than MAX_AGE_SECONDS. Paged (PR_PAGE runs
                  # a page) through the raw API, as pass 2 is: one `kubectl get pipelinerun -o json`
                  # of every run was 53 MB on 2026-11-15 and kubectl needed about 563 MB to decode
                  # it under a 256Mi limit. The job was OOMKilled at this line every hour from
                  # 04:21Z for 43 hours; nothing after it ran, finished pods piled up to 5,900 and
                  # workspaces to 717, and their series took PROD's Prometheus down with them.
                  # Each page leaves only the verdict lines and, for pass 4, uid and completion time.
                  : >"${work}/pr.sel"
                  : >"${work}/pr.slim"
                  cont=""
                  while :; do
                    url="/apis/tekton.dev/v1/namespaces/${NS}/pipelineruns?limit=${PR_PAGE:-100}"
                    [ -n "${cont}" ] && url="${url}&continue=${cont}"
                    kubectl get --raw "${url}" >"${work}/page.json"
                    jq -r --argjson now "${NOW}" --argjson max "${MAX_AGE_SECONDS}" '
                      .items[]
                      | select(.status.completionTime != null)
                      | (try (.status.completionTime | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) catch null) as $done
                      | if $done != null and ($now - $done) >= $max then "D " + .metadata.name else "K" end
                    ' "${work}/page.json" >>"${work}/pr.sel"
                    jq -c '.items[]
                      | select(.status.completionTime != null and .metadata.uid != null)
                      | {uid: .metadata.uid, done: .status.completionTime}
                    ' "${work}/page.json" >>"${work}/pr.slim"
                    cont="$(jq -r '.metadata.continue // empty | @uri' "${work}/page.json")"
                    [ -z "${cont}" ] && break
                  done
                  deleted=0
                  while read -r tag name; do
                    [ "${tag}" = D ] || continue
                    echo "delete PipelineRun ${name}"
                    kubectl delete pipelinerun "${name}" -n "${NS}" --wait=false ${DRY}
                    deleted=$((deleted + 1))
                  done <"${work}/pr.sel"
                  kept=$(grep -c '^K' "${work}/pr.sel" || true)
                  echo "pipelinerun-ttl done${DRY:+ (dry-run)} deleted=${deleted} kept=${kept} maxAge=${MAX_AGE_SECONDS}s"
                  # Pass 2: pods of finished TaskRuns, Succeeded after POD_MAX_AGE_SECONDS,
                  # Failed after FAILED_POD_MAX_AGE_SECONDS. Paged (250 pods/page) through the
                  # raw API and filtered with jq: decoding the whole list in one `kubectl get` was
                  # OOMKilled at 256Mi with ~3,300 pods.
                  : >"${work}/pods.del"
                  pkept=0
                  cont=""
                  while :; do
                    url="/api/v1/namespaces/${NS}/pods?limit=250"
                    [ -n "${cont}" ] && url="${url}&continue=${cont}"
                    kubectl get --raw "${url}" >"${work}/page.json"
                    jq -r --argjson now "${NOW}" --argjson ok "${POD_MAX_AGE_SECONDS}" --argjson bad "${FAILED_POD_MAX_AGE_SECONDS}" '
                      .items[]
                      | select(.metadata.ownerReferences[0].kind? == "TaskRun")
                      | select(.status.phase == "Succeeded" or .status.phase == "Failed")
                      | ([.status.containerStatuses[]?.state.terminated.finishedAt // empty] | max) as $f
                      | select($f != null)
                      | (if .status.phase == "Failed" then $bad else $ok end) as $max
                      | (try ($f | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) catch null) as $fe
                      | if $fe != null and ($now - $fe) >= $max then "D " + .metadata.name else "K" end
                    ' "${work}/page.json" >"${work}/page.sel"
                    grep '^D ' "${work}/page.sel" | cut -c3- >>"${work}/pods.del" || true
                    pkept=$((pkept + $(grep -c '^K' "${work}/page.sel" || true)))
                    cont="$(jq -r '.metadata.continue // empty | @uri' "${work}/page.json")"
                    [ -z "${cont}" ] && break
                  done
                  pdeleted=$(wc -l <"${work}/pods.del" | tr -d ' ')
                  xargs -r -n 50 kubectl delete pod -n "${NS}" --wait=false ${DRY} <"${work}/pods.del" >/dev/null
                  echo "taskrun-pod-ttl done${DRY:+ (dry-run)} deleted=${pdeleted} kept=${pkept} maxAge=${POD_MAX_AGE_SECONDS}s/${FAILED_POD_MAX_AGE_SECONDS}s"
                  # Pass 3: standalone TaskRuns (no tekton.dev/pipelineRun label) older than
                  # MAX_AGE_SECONDS, keeping the newest KEEP_PER_TASK finished ones of each task.
                  # Paged like passes 1 and 2 (950 of them on 2026-11-16, and unit tasks add more
                  # every run): each page leaves name, task and completion time.
                  : >"${work}/tr.slim"
                  ttotal=0
                  cont=""
                  while :; do
                    url="/apis/tekton.dev/v1/namespaces/${NS}/taskruns?labelSelector=%21tekton.dev%2FpipelineRun&limit=${PR_PAGE:-100}"
                    [ -n "${cont}" ] && url="${url}&continue=${cont}"
                    kubectl get --raw "${url}" >"${work}/page.json"
                    ttotal=$((ttotal + $(jq '.items | length' "${work}/page.json")))
                    jq -c '.items[]
                      | select(.status.completionTime != null)
                      | {name: .metadata.name,
                         task: (.metadata.labels["tekton.dev/task"] // .spec.taskRef.name // "-"),
                         done: (try (.status.completionTime | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) catch null)}
                      | select(.done != null)
                    ' "${work}/page.json" >>"${work}/tr.slim"
                    cont="$(jq -r '.metadata.continue // empty | @uri' "${work}/page.json")"
                    [ -z "${cont}" ] && break
                  done
                  jq -r -s --argjson now "${NOW}" --argjson max "${MAX_AGE_SECONDS}" --argjson keep "${KEEP_PER_TASK}" '
                    group_by(.task)
                    | map(sort_by(-.done) | .[$keep:] | map(select(($now - .done) >= $max)))
                    | flatten
                    | .[].name
                  ' "${work}/tr.slim" >"${work}/tr.del"
                  tdeleted=$(wc -l <"${work}/tr.del" | tr -d ' ')
                  xargs -r -n 50 kubectl delete taskrun -n "${NS}" --wait=false ${DRY} <"${work}/tr.del" >/dev/null
                  echo "standalone-taskrun-ttl done${DRY:+ (dry-run)} deleted=${tdeleted} of=${ttotal} keepPerTask=${KEEP_PER_TASK} maxAge=${MAX_AGE_SECONDS}s"
                  # Pass 4: workspace PVCs owned by a PipelineRun that finished more than
                  # WORKSPACE_MAX_AGE_SECONDS ago (matched by owner uid; the run object stays).
                  # Runs pass 1 deleted take their PVCs with them through garbage collection.
                  kubectl get pvc -n "${NS}" -o json >"${work}/pvc.json"
                  jq -r --argjson now "${NOW}" --argjson ws "${WORKSPACE_MAX_AGE_SECONDS}" --slurpfile pr "${work}/pr.slim" '
                    ([$pr[]
                      | (try (.done | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) catch null) as $done
                      | select($done != null and ($now - $done) >= $ws)
                      | {(.uid): true}] | add // {}) as $old
                    | .items[]
                    | select([.metadata.ownerReferences[]? | select(.kind == "PipelineRun" and ($old[.uid] // false))] | length > 0)
                    | .metadata.name
                  ' "${work}/pvc.json" >"${work}/pvc.del"
                  wdeleted=$(wc -l <"${work}/pvc.del" | tr -d ' ')
                  wtotal=$(jq '.items | length' "${work}/pvc.json")
                  xargs -r -n 50 kubectl delete pvc -n "${NS}" --wait=false --ignore-not-found ${DRY} <"${work}/pvc.del" >/dev/null
                  echo "workspace-pvc-ttl done${DRY:+ (dry-run)} deleted=${wdeleted} of=${wtotal} maxAge=${WORKSPACE_MAX_AGE_SECONDS}s"
TTL_CRONJOB_EOF
kubectl -n cicd get cronjob tekton-pipelinerun-ttl -o jsonpath='{.metadata.generation} {.spec.jobTemplate.spec.template.spec.containers[0].resources.limits.memory}{"\n"}'; kubectl -n cicd get cronjob tekton-pipelinerun-ttl -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].command[2]}' | grep -c 'pipelineruns?limit' 
