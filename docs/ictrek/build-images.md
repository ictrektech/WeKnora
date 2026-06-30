# WeKnora 镜像构建

本文件记录 ictrek 的 WeKnora 镜像构建流程。中文说明在上方，英文原文在下方。

构建范围只包含 WeKnora 自有镜像：

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

vLLM、Ollama、model_hub 等模型后端不在这个构建流程里。它们使用官方镜像或各自组件的镜像流程。

WeKnora 这三个镜像本身不包含 CUDA 运行时依赖，所以 tag 不应带 `cu130` 之类 CUDA 标记。当前推荐 tag：

```text
amd_YYYYMMDD
arm_YYYYMMDD
```

构建前不要在远程构建机上跑 git。先把本地工作树同步过去：

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

然后在构建机执行：

```bash
ssh <build-host> 'bash -s' <<'EOF'
set -euo pipefail
cd /data/jhu/build/weknora
chmod +x build_image.sh
./build_image.sh --target amd
EOF
```

只构建单个服务镜像时使用：

```bash
./build_image.sh --app-only
./build_image.sh --frontend-only
./build_image.sh --docreader-only
```

`--no-push` 用于只做本机构建检查；`--no-feishu` 用于不更新飞书发布表。

飞书发布表规则：

- 凭证在构建机 `~/.feishu.json`，不要提交或打印；
- 表格 token：`Htotsn3oahO1zxt73YMcaB1zn8e`；
- amd 目标默认更新 `AMD_with_cuda`、`AMD_with_mxn100`；
- arm 目标默认更新 `ARM_without_cuda`、`l4t`、`ARM_with_cuda`、`thor_spark`、`SOPHON_bm1688`；
- 每个服务镜像一列：`weknora`、`weknora-ui`、`weknora-docreader`；
- 第 1 行是服务名，第 2 行是镜像仓库地址，日期行写 tag，完整镜像是 `<row-2-repository>:<date-row-tag>`；
- 脚本先复用已有服务列，不存在才追加下一空列；构建脚本不能删除或整理飞书列。

如果镜像已经在 SWR 和飞书表中存在，部署时走“使用已有镜像”路径：在对应平台 sheet 中找到三个服务列，组合第 2 行仓库和日期行 tag，生成 `docker-compose.images.yml`，然后用：

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

真实部署时不要只复制上面的 `-f` 示例。新机建议在 `.env` 固定：

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.override.yml:docker-compose.images.yml
```

已有部署如果一直使用 named volume，则保持原来的 `COMPOSE_FILE`，不要为了换镜像临时加减 `docker-compose.override.yml`。启动后必须按 [remote-weknora-deployment.md](remote-weknora-deployment.md#升级后的强制冒烟检查) 做“你是谁”、文档问答、SSRF 白名单检查。

发布镜像不包含部署专用模型默认值。`config/builtin_models.yaml` 在镜像内默认是空的，`docker-compose.override.yml` 也不会默认挂载模型文件。模型应在 Web UI 后配，或由运维人员显式挂载基于 `.env` 的 `config/builtin_models.yaml`。

注意：如果用空 `builtin_models.yaml` 覆盖旧部署，先检查数据库里模型行的 `managed_by`。仍为 `managed_by='yaml'` 且不在当前 YAML 中的模型行，会在 app 启动时被软删除。需要长期保留的运行时模型，要么继续写在挂载 YAML 中，要么改成 `managed_by=''` 的手工行。

---

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

For real deployments, do not blindly copy the `-f` example above. On a fresh
host, pin this in `.env`:

```env
COMPOSE_FILE=docker-compose.yml:docker-compose.override.yml:docker-compose.images.yml
```

If an existing deployment has always used named volumes, keep its original
`COMPOSE_FILE` and do not add or remove `docker-compose.override.yml` just to
upgrade images. After startup, run the mandatory smoke checks in
`remote-weknora-deployment.md`: ask "你是谁", test document QA, and confirm the
SSRF allowlist still permits the configured model backends.

`docker-compose.override.yml` keeps local persistent mappings such as
`./data/files`, `./data/postgres`, and `./data/redis`. If a deployment does not
use that override file, add equivalent host mappings before starting the
service.

The released WeKnora images do not contain deployment-specific model defaults.
`config/builtin_models.yaml` in the image is intentionally empty, and
`docker-compose.override.yml` does not mount a model file by default. Operators
must add models later in the Web UI or explicitly mount an operator-created
`config/builtin_models.yaml` that reads model names and endpoints from `.env`.

When deploying an image whose `config/builtin_models.yaml` is empty over an
older deployment, check existing model rows first. Any row still marked
`managed_by='yaml'` but absent from the current YAML is soft-deleted when the
app starts. Persistent runtime model rows should either remain in the mounted
YAML or be recreated/converted to manual rows with `managed_by=''`.

For Ollama-only deployments, set `OLLAMA_BASE_URL` in `.env` and create local
model rows (`source: local`) for chat, VLM, and embedding. For OpenAI-compatible
remote backends, create remote rows (`source: remote`) with a `/v1` base URL.
See `remote-weknora-deployment.md` and `model-hub-ollama-embedding.md` for the
full variable-driven examples.
