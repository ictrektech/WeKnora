#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ictrek-dev.sh" help >/dev/null

# Stub discovery so these checks do not depend on an installed Scheduler.
scheduler_available() { return 0; }
[[ "$(resolve_profile auto)" == scheduler ]]
[[ "$(resolve_profile tc232)" == tc232 ]]
scheduler_available() { return 1; }
ICTREK_DEV_HOST_ARCH=x86_64
[[ "$(resolve_profile auto)" == tc232 ]]

ICTREK_DEV_PROFILE=scheduler
ICTREK_DEV_SCHEDULER_URL=http://127.0.0.1:24888/v1/
ICTREK_DEV_SCHEDULER_API_KEY=fixture-key
ICTREK_DEV_MODEL_BACKEND=model-hub
ICTREK_DEV_OLLAMA_BASE_URL=http://127.0.0.1:31535
refresh_config
[[ "$DEV_MODEL_BACKEND" == scheduler ]]
[[ "$DEV_SCHEDULER_BASE_URL" == http://127.0.0.1:24888/v1 ]]
[[ "$HYBRAG_V_ROUTER_URL" == http://127.0.0.1:24888 ]]
[[ "$HYBRAG_V_ROUTER_API_KEY" == fixture-key ]]
[[ "$HYBRAG_MODEL_BACKEND" == scheduler ]]
[[ "$OLLAMA_BASE_URL" == http://127.0.0.1:24888 ]]
[[ "$(default_model_config_for_profile scheduler)" == *builtin_models.scheduler.yaml ]]

ENV_FILE="$(mktemp)"
trap 'rm -f -- "$ENV_FILE"' EXIT
printf 'JWT_SECRET=keep-secret\nDB_PASSWORD=keep-password\n' > "$ENV_FILE"
setup_env --profile scheduler >/dev/null
[[ "$(get_env_value ICTREK_DEV_PROFILE)" == scheduler ]]
[[ "$(get_env_value HYBRAG_MODEL_BACKEND)" == scheduler ]]
[[ "$(get_env_value ICTREK_DEV_SCHEDULER_BASE_URL)" == http://127.0.0.1:24888/v1 ]]
[[ "$(get_env_value JWT_SECRET)" == keep-secret ]]
[[ "$(get_env_value DB_PASSWORD)" == keep-password ]]
setup_env --profile tc232 >/dev/null
[[ "$(get_env_value HYBRAG_MODEL_BACKEND)" == model-hub ]]
echo 'PASS: Scheduler discovery, explicit profiles, gateway normalization, migration and preserved secrets'
