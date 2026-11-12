#!/usr/bin/env bash
# Throwaway PostgreSQL (and Redis) for the agentrt database tests on a
# developer machine, configured as CI's sidecars in
# bifrost-trade-infra/k8s/cicd/tekton/task-ci-platform-test-api.yaml:
# fsync, full-page writes and synchronous commit off, 300 connections, trust
# auth, and the same login roles. Without AGENTRT_TEST_PG the database tests
# skip, so a local green run without this server tested nothing.
#
#   scripts/test-db.sh start    # init (first time) and start, idempotent
#   scripts/test-db.sh env      # print the exports; eval "$(scripts/test-db.sh env)"
#   scripts/test-db.sh status
#   scripts/test-db.sh stop
#
# Data lives in ~/.cache/bifrost-test-pg (BIFROST_TEST_PG_DIR). It listens on
# 127.0.0.1 only, port 55432 (BIFROST_TEST_PG_PORT); Redis on 56379
# (BIFROST_TEST_REDIS_PORT). It never touches another server and reads no
# secret. Homebrew's postgresql@17 is used when present, else Docker.
set -euo pipefail

dir=${BIFROST_TEST_PG_DIR:-$HOME/.cache/bifrost-test-pg}
port=${BIFROST_TEST_PG_PORT:-55432}
redis_port=${BIFROST_TEST_REDIS_PORT:-56379}
container=bifrost-test-pg
redis_container=bifrost-test-redis
pg_image=postgres:17
redis_image=redis:7-alpine

roles_sql=(
  -c "CREATE ROLE agentrt_owner_dev LOGIN" -c "CREATE ROLE platform_agentrt_dev LOGIN"
  -c "CREATE ROLE platform_agentrt_stg LOGIN" -c "CREATE ROLE agent_reader LOGIN"
)
server_opts=(-c fsync=off -c full_page_writes=off -c synchronous_commit=off -c max_connections=300)

pg_bin=
for d in /opt/homebrew/opt/postgresql@17/bin /usr/local/opt/postgresql@17/bin; do
  if [[ -x $d/pg_ctl && -x $d/initdb && -x $d/postgres ]]; then pg_bin=$d; break; fi
done
redis_bin=$(command -v redis-server || true)

die() { echo "test-db: $*" >&2; exit 1; }

mode() {
  if [[ -n $pg_bin ]]; then echo native
  elif command -v docker >/dev/null 2>&1; then echo docker
  else die "need Homebrew postgresql@17 (brew install postgresql@17) or docker"
  fi
}

pg_running() {
  case $(mode) in
    native) [[ -f $dir/data/postmaster.pid ]] && "$pg_bin/pg_ctl" -D "$dir/data" status >/dev/null 2>&1 ;;
    docker) [[ $(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null) == true ]] ;;
  esac
}

redis_running() {
  if [[ -n $redis_bin ]]; then
    [[ -f $dir/redis.pid ]] && kill -0 "$(cat "$dir/redis.pid")" 2>/dev/null
  else
    [[ $(docker inspect -f '{{.State.Running}}' "$redis_container" 2>/dev/null) == true ]]
  fi
}

# server_version is the server's version, read once it runs: it goes into
# AGENTRT_TEST_PG so Go's test cache, which keys on the environment the tests
# read, replays nothing measured against another version.
server_version() {
  case $(mode) in
    native) "$pg_bin/psql" -h 127.0.0.1 -p "$port" -U postgres -d postgres -XtAc 'SHOW server_version_num' ;;
    docker) docker exec "$container" psql -U postgres -d postgres -XtAc 'SHOW server_version_num' ;;
  esac
}

start_pg() {
  if pg_running; then return; fi
  mkdir -p "$dir"
  case $(mode) in
    native)
      if [[ ! -f $dir/data/PG_VERSION ]]; then
        LC_ALL=C "$pg_bin/initdb" -D "$dir/data" -U postgres --auth=trust -E UTF8 --locale=C >/dev/null
        rm -f "$dir/roles-ready"
      fi
      LC_ALL=C "$pg_bin/pg_ctl" -D "$dir/data" -l "$dir/log" \
        -o "-c listen_addresses=127.0.0.1 -p $port -k $dir ${server_opts[*]}" -w start >/dev/null
      if [[ ! -f $dir/roles-ready ]]; then
        "$pg_bin/psql" -h 127.0.0.1 -p "$port" -U postgres -d postgres -X -v ON_ERROR_STOP=1 -q "${roles_sql[@]}"
        touch "$dir/roles-ready"
      fi
      ;;
    docker)
      if docker inspect "$container" >/dev/null 2>&1; then
        docker start "$container" >/dev/null
      else
        docker run -d --name "$container" -p "127.0.0.1:$port:5432" \
          -e POSTGRES_HOST_AUTH_METHOD=trust -e LC_ALL=C "$pg_image" "${server_opts[@]}" >/dev/null
      fi
      for _ in $(seq 60); do
        docker exec "$container" pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1 && break
        sleep 1
      done
      docker exec "$container" pg_isready -U postgres -h 127.0.0.1 >/dev/null || die "postgres container did not become ready"
      # The roles are created once; a restarted container keeps them.
      if ! docker exec "$container" psql -U postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname = 'agent_reader'" | grep -q 1; then
        docker exec "$container" psql -U postgres -d postgres -X -v ON_ERROR_STOP=1 -q "${roles_sql[@]}"
      fi
      ;;
  esac
}

start_redis() {
  if redis_running; then return; fi
  if [[ -n $redis_bin ]]; then
    "$redis_bin" --bind 127.0.0.1 --port "$redis_port" --save "" --appendonly no \
      --daemonize yes --pidfile "$dir/redis.pid" --logfile "$dir/redis.log" --dir "$dir" >/dev/null
    for _ in $(seq 20); do [[ -f $dir/redis.pid ]] && break; sleep 0.2; done
  elif command -v docker >/dev/null 2>&1; then
    if docker inspect "$redis_container" >/dev/null 2>&1; then
      docker start "$redis_container" >/dev/null
    else
      docker run -d --name "$redis_container" -p "127.0.0.1:$redis_port:6379" "$redis_image" \
        redis-server --save "" --appendonly no >/dev/null
    fi
  else
    die "need redis-server (brew install redis) or docker"
  fi
}

stop_all() {
  case $(mode) in
    native) if pg_running; then "$pg_bin/pg_ctl" -D "$dir/data" -m fast -w stop >/dev/null; fi ;;
    docker) docker stop "$container" >/dev/null 2>&1 || true ;;
  esac
  if [[ -n $redis_bin ]]; then
    if redis_running; then kill "$(cat "$dir/redis.pid")"; fi
    rm -f "$dir/redis.pid"
  else
    docker stop "$redis_container" >/dev/null 2>&1 || true
  fi
}

print_env() {
  pg_running || die "not running; run: scripts/test-db.sh start"
  local v
  v=$(server_version)
  echo "export AGENTRT_TEST_PG='host=127.0.0.1 port=$port user=postgres sslmode=disable application_name=agentrt-test-pg$v'"
  if redis_running; then
    echo "export AGENTRT_TEST_REDIS='redis://127.0.0.1:$redis_port/0'"
    echo "export AGENTRT_TEST_REQUIRED=1"
  fi
}

case ${1:-} in
  start)
    start_pg
    start_redis
    echo "test-db: postgres on 127.0.0.1:$port ($(mode)), redis on 127.0.0.1:$redis_port; data in $dir" >&2
    echo "test-db: eval \"\$(scripts/test-db.sh env)\"" >&2
    ;;
  stop) stop_all ;;
  env) print_env ;;
  status)
    if pg_running; then echo "postgres: running on 127.0.0.1:$port ($(mode), $(server_version))"; else echo "postgres: stopped"; fi
    if redis_running; then echo "redis: running on 127.0.0.1:$redis_port"; else echo "redis: stopped"; fi
    ;;
  *) die "usage: $0 start|stop|env|status" ;;
esac
