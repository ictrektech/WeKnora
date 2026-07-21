# WeKnora 镜像构建

本文件记录 ictrek 的 WeKnora 镜像构建流程。中文说明在上方，英文原文在下方。

构建范围只包含 WeKnora 自有镜像：

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:<tag>
```

vLLM、Ollama、model_hub 等模型后端不在这个构建流程里。它们使用官方镜像或各自组件的镜像流程。

WeKnora 这些镜像本身不包含 CUDA 运行时依赖，所以 tag 不应带 `cu130` 之类 CUDA 标记。当前推荐 tag：

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

构建同步目录只用于构建镜像，不是部署目录。不要在 `/data/jhu/build/weknora` 里执行 `docker compose pull`、`docker compose up` 或重启运行服务，除非已经确认该目录就是当前运行容器的 compose project。源码默认 compose 里仍可能保留上游默认 image/build 配置；在构建目录误跑 compose 会拉取或启动上游镜像，而不是 ictrek 的 SWR 发布镜像。

只构建单个服务镜像时使用：

```bash
./build_image.sh --app-only
./build_image.sh --frontend-only
./build_image.sh --docreader-only
./build_image.sh --sandbox-only
```

`--no-push` 用于只做本机构建检查；`--no-feishu` 用于不更新飞书发布表。

飞书发布表规则：

- 凭证在构建机 `~/.feishu.json`，不要提交或打印；
- 表格 token：`Htotsn3oahO1zxt73YMcaB1zn8e`；
- amd 目标默认更新 `AMD_with_cuda`、`AMD_with_mxn100`；
- arm 目标默认更新 `ARM_without_cuda`、`l4t`、`ARM_with_cuda`、`thor_spark`、`SOPHON_bm1688`；
- 每个服务镜像一列：`weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox`；
- 第 1 行是服务名，第 2 行是镜像仓库地址，日期行写 tag，完整镜像是 `<row-2-repository>:<date-row-tag>`；
- 脚本会先在已读取表头范围内查找同名服务列；服务列不存在时，只追加到从 B 列开始的连续组件块之后第一个空列，不能跳到远端空列继续写；
- 构建脚本不能删除或整理飞书列。历史空列或误写远端列只能通过飞书 UI 或一次性维护脚本单独处理。

如果镜像已经在 SWR 和飞书表中存在，部署时不要从源码根目录临时拼 compose 文件。到对应平台 sheet 中找到四个服务列，组合第 2 行仓库和日期行 tag，然后写入部署目录 `.env`：

```env
WEKNORA_APP_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
WEKNORA_UI_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
WEKNORA_DOCREADER_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
WEKNORA_SANDBOX_DOCKER_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:<tag>
```

实际部署由 VOS app 安装包中的 `ictrek.app/src/docker-compose.yml` 渲染完成。构建文档只记录镜像构建、推送和飞书写入，不维护运行时 compose 示例。旧独立部署模板已归档到 [legacy](legacy/) 仅供旧环境排查。

升级已有部署前，先用正在运行的 app 容器确认真实部署目录和 compose 文件集合：

```bash
docker inspect <app-container> --format \
  'project={{index .Config.Labels "com.docker.compose.project"}} workdir={{index .Config.Labels "com.docker.compose.project.working_dir"}} config={{index .Config.Labels "com.docker.compose.project.config_files"}}'
```

只在输出的 `workdir` 目录中执行 `docker compose pull/up/restart`。如果 compose 文件没有指向 `swr.cn-southwest-2.myhuaweicloud.com/ictrek/...`，先修正 image override，不要继续执行。

发布镜像默认不会主动创建部署专用模型行。VOS HybRAG 安装包通过环境变量 `HYBRAG_DEFAULT_BUILTIN_MODELS=true` 让 App 容器入口脚本在运行时生成 `builtin_models.yaml`，用于创建可区分 QA/VLM Ollama 和 embedding Ollama 的默认模型行；VOS 包内不要放额外 `config/` 目录，否则当前 VOS parser 会拒绝解析。非 VOS 部署仍应在 Web UI 后配，或由运维人员显式挂载基于 `.env` 的 `config/builtin_models.yaml`。

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
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:<tag>
```

The images do not contain CUDA runtime dependencies, so tags should not include
CUDA markers. Current target prefixes are:

```text
amd_YYYYMMDD
arm_YYYYMMDD
```

Even when the deployment host has CUDA-capable GPUs, the WeKnora app, frontend,
docreader, and sandbox images should not use a `cu130` tag unless the image
itself starts depending on CUDA libraries.

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

The synced build directory is only for building images. Do not run
`docker compose pull`, `docker compose up`, or runtime restarts from
`/data/jhu/build/weknora` unless that directory has been confirmed as the
actual running compose project. The source compose files may still contain
upstream default image/build settings; running compose from the build directory
can pull or start upstream images instead of ictrek SWR release images.

To build only one service image:

```bash
./build_image.sh --app-only
./build_image.sh --frontend-only
./build_image.sh --docreader-only
./build_image.sh --sandbox-only
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
weknora-sandbox    -> swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox
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
weknora-sandbox
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
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:arm_20260626
```

Write those three full image names into the deployment `.env`:

```env
WEKNORA_APP_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
WEKNORA_UI_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
WEKNORA_DOCREADER_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
WEKNORA_SANDBOX_DOCKER_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-sandbox:<tag>
```

Runtime deployment is rendered from `ictrek.app/src/docker-compose.yml` by the
VOS app package. This build document only records image builds, pushes, and
Feishu release table updates. Standalone compose templates are archived under
`legacy/` for old-environment diagnosis.

Before upgrading an existing deployment, identify the real deployment directory
and compose file set from the running app container:

```bash
docker inspect <app-container> --format \
  'project={{index .Config.Labels "com.docker.compose.project"}} workdir={{index .Config.Labels "com.docker.compose.project.working_dir"}} config={{index .Config.Labels "com.docker.compose.project.config_files"}}'
```

Run `docker compose pull/up/restart` only from the reported `workdir`. If the
compose file does not point to `swr.cn-southwest-2.myhuaweicloud.com/ictrek/...`,
fix the image override before continuing.

The ictrek deployment template already includes local persistent mappings such
as `./data/files`, `./data/postgres`, and `./data/redis`. Keep using the same
deployment directory for upgrades so the app keeps seeing the same database and
file storage.

The released WeKnora images do not contain deployment-specific model defaults.
The VOS HybRAG package sets `HYBRAG_DEFAULT_BUILTIN_MODELS=true`, so the app
entrypoint generates `builtin_models.yaml` at runtime for distinct QA/VLM
Ollama and embedding Ollama model rows. Do not put an extra `config/` directory
into the VOS package; the current VOS parser rejects it. Non-VOS deployments
must still add models later in the Web UI or explicitly mount an
operator-created `config/builtin_models.yaml` that reads model names and
endpoints from `.env`.

When deploying an image whose `config/builtin_models.yaml` is empty over an
older deployment, check existing model rows first. Any row still marked
`managed_by='yaml'` but absent from the current YAML is soft-deleted when the
app starts. Persistent runtime model rows should either remain in the mounted
YAML or be recreated/converted to manual rows with `managed_by=''`.

For current VOS deployments, HybRAG uses Model Hub services
`model-hub-ollama-qa` and `model-hub-ollama-embedding` through the `11535/v1`
Gateway. See `../README.md` and `vos-ollama-prewarm.md` for the current runtime
details. Old Ollama-only and remote-backend examples are kept under `legacy/`.
