#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ictrek-dev.sh" help >/dev/null

DEV_DATA_DIR="$(mktemp -d)"
trap 'rm -rf -- "$DEV_DATA_DIR"' EXIT
DEV_DB_PORT=15432
DEV_REDIS_PORT=6380
DEV_DOCREADER_PORT=15051
DEV_NEO4J_HTTP_PORT=27474
DEV_NEO4J_BOLT_PORT=27687

check_compose() {
    docker compose --env-file /dev/null -f "$COMPOSE_FILE" config --format json | python3 -c '
import json, sys
postgres = json.load(sys.stdin)["services"]["postgres"]
assert postgres["image"] == sys.argv[1], postgres["image"]
assert postgres["command"] == ["postgres", "-c", "shared_preload_libraries=pg_search,pg_cron,vchord.so,vectors.so"]
assert "-h 127.0.0.1" in postgres["healthcheck"]["test"][1]
' "$1"
}

for arch in amd64 arm64; do
    unset ICTREK_DEV_PGV_IMAGE
    ICTREK_DEV_HOST_ARCH="$arch"
    export_compose_env
    [[ "$ICTREK_DEV_PGV_IMAGE" == *":${arch%64}_17_20261014" ]]
    check_compose "$ICTREK_DEV_PGV_IMAGE"
done
ICTREK_DEV_PGV_IMAGE=explicit-pgv-image
export_compose_env
[[ "$ICTREK_DEV_PGV_IMAGE" == explicit-pgv-image ]]
check_compose explicit-pgv-image
unset ICTREK_DEV_PGV_IMAGE
check_compose swr.cn-southwest-2.myhuaweicloud.com/ictrek/pgv:amd_17_20261014
(
    load_env() { return 0; }
    refresh_config() { DEV_MODEL_BACKEND=ollama; }
    check_docker() { return 0; }
    compose() { return 0; }
    wait_for_service() { [[ "$1" != postgres ]]; }
    if start_services --no-neo4j >/dev/null; then
        echo 'FAIL: start reported success despite PostgreSQL health failure' >&2
        exit 1
    fi
)
echo 'PASS: AMD/ARM image selection, explicit override, Compose defaults, preloads and TCP health check'
echo 'PASS: PostgreSQL health failure propagates from start'
