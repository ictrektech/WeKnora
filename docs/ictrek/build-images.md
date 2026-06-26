# WeKnora Image Build

This note records the ictrek image build flow for WeKnora service images.

The build flow only covers WeKnora-owned images. External model backends such
as vLLM, Ollama, and model_hub are not built here; use their official or
component-specific image flow instead.

## Images

`build_image.sh` builds and pushes these images:

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

The images do not contain CUDA runtime dependencies, so tags should not include
CUDA markers. Current target prefixes are:

```text
amd_YYYYMMDD
arm_YYYYMMDD
```

Even when the deployment host has CUDA-capable GPUs, the WeKnora app, frontend,
and docreader images should not use a `cu130` tag unless the image itself starts
depending on CUDA libraries.

## Build

Do not run git on the remote build host. Sync the local working tree first:

```bash
rsync -az --delete \
  --exclude '.git' \
  --exclude 'frontend/node_modules' \
  --exclude 'frontend/dist' \
  --exclude 'data' \
  --exclude '.cache' \
  --exclude '.env' \
  apps/WeKnora/ <build-host>:/data/jhu/build/weknora/
```

Then build and push from the synced tree:

```bash
ssh <build-host> 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/build/weknora
chmod +x build_image.sh
./build_image.sh --target amd
EOF
```

To build only one service image:

```bash
./build_image.sh --app-only
./build_image.sh --frontend-only
./build_image.sh --docreader-only
```

Use `--no-push` for a local build check and `--no-feishu` when the image should
not update the release table.

The script defaults to reachable mirrors for remote builds:

```text
GOPROXY_ARG=https://goproxy.cn,direct
APK_MIRROR_ARG=mirrors.tuna.tsinghua.edu.cn
APT_MIRROR=http://mirrors.tuna.tsinghua.edu.cn
NPM_REGISTRY=https://registry.npmmirror.com
```

For ARM checks, sync the same source tree to an ARM build host and run:

```bash
./build_image.sh --target arm --no-push --no-feishu
```

## Feishu Release Table

The script reads credentials from `~/.feishu.json` on the build host. Do not
commit or print the credential values.

The release spreadsheet token is:

```text
Htotsn3oahO1zxt73YMcaB1zn8e
```

Default Feishu sheet updates:

```text
amd target: AMD_with_cuda, AMD_with_mxn100
arm target: ARM_without_cuda, l4t, ARM_with_cuda, thor_spark, SOPHON_bm1688
```

The table uses one service column per image:

```text
weknora            -> swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora
weknora-ui         -> swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui
weknora-docreader  -> swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader
```

If a column is missing, `build_image.sh` creates the service header and writes
the repository URI in the second row. After all selected images are pushed, the
script writes the generated tag to each selected service column for the current
date row. New service columns are not tied to fixed letters such as `AE`,
`AF`, or `AG`; the script first reuses an existing service column and otherwise
appends the next empty column after the current component block.

The build script must not delete or compact Feishu columns. If a sheet needs
manual cleanup, handle that separately through the Feishu UI or a one-off
maintenance script.

## Start From Existing Images

Use this path when the required WeKnora images already exist in SWR and are
recorded in the Feishu release table. This does not build images.

In the Feishu release table, select the platform sheet that matches the target
host, then read these service columns:

```text
weknora
weknora-ui
weknora-docreader
```

For each service, row 2 is the image repository and the selected date row is
the tag. The full image name is:

```text
<row-2-repository>:<date-row-tag>
```

For example, the ARM record for `20260626` currently resolves to:

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:arm_20260626
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:arm_20260626
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:arm_20260626
```

Create a local image override file such as `docker-compose.images.yml`:

```yaml
services:
  frontend:
    image: swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:arm_20260626
    build: null
  app:
    image: swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:arm_20260626
    build: null
  docreader:
    image: swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:arm_20260626
    build: null
```

Start with the existing-image override:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.images.yml \
  pull frontend app docreader

docker compose \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f docker-compose.images.yml \
  up -d postgres redis docreader app frontend
```

`docker-compose.override.yml` keeps local persistent mappings such as
`./data/files`, `./data/postgres`, and `./data/redis`. If a deployment does not
use that override file, add equivalent host mappings before starting the
service.

The released WeKnora images do not contain deployment-specific model defaults.
`config/builtin_models.yaml` in the image is intentionally empty, and
`docker-compose.override.yml` does not mount a model file by default. Operators
must add models later in the Web UI or explicitly mount an operator-created
`config/builtin_models.yaml` that reads model names and endpoints from `.env`.

For Ollama-only deployments, set `OLLAMA_BASE_URL` in `.env` and create local
model rows (`source: local`) for chat, VLM, and embedding. For OpenAI-compatible
remote backends, create remote rows (`source: remote`) with a `/v1` base URL.
See `remote-weknora-deployment.md` and `model-hub-ollama-embedding.md` for the
full variable-driven examples.
