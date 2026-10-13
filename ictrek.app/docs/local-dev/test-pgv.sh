#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ictrek-dev.sh" help >/dev/null

DEV_DATA_DIR=/tmp/weknora-pgv-test
DEV_DB_PORT=15432
DEV_REDIS_PORT=6380
DEV_DOCREADER_PORT=15051
DEV_NEO4J_HTTP_PORT=27474
DEV_NEO4J_BOLT_PORT=27687

for arch in amd64 arm64; do
    unset ICTREK_DEV_PGV_IMAGE
    ICTREK_DEV_HOST_ARCH="$arch"
    export_compose_env
    [[ "$ICTREK_DEV_PGV_IMAGE" == *":${arch%64}_16_20261012" ]]
done
ICTREK_DEV_PGV_IMAGE=explicit-pgv-image
export_compose_env
[[ "$ICTREK_DEV_PGV_IMAGE" == explicit-pgv-image ]]
echo 'PASS: AMD/ARM image selection and explicit image override'
