# ictrek WeKnora 部署模板

本目录是 ictrek 远程部署模板。本文只保留中文说明。

这些文件用于部署已发布镜像：

```text
docker-compose.yml
.env.example
config/builtin_models.yaml.example
deploy.sh
update-and-deploy.sh
trigger-reparse-incomplete.sh
```

Orin NX / L4T 机器如果要用纯 Ollama 后端，额外使用：

```text
docker-compose.orin-ollama.yml
.env.orin-ollama.example
config/builtin_models.orin-ollama.yaml.example
```

这个 overlay 会启动两个 Ollama 容器：`ollama-qa` 只服务聊天和图片理解，`ollama-embedding` 只服务 embedding。这样文档向量化不会把聊天/VLM 的 Ollama 调度槽位吃满。

Orin NX / Jetson 上这两个 Ollama 容器显式使用 `runtime: nvidia`、`NVIDIA_VISIBLE_DEVICES=all` 和 `NVIDIA_DRIVER_CAPABILITIES=compute,utility`，避免按 Docker 默认 `runc` 启动后模型落到 CPU。部署后用下面命令确认：

```bash
docker inspect ollama-qa --format 'runtime={{.HostConfig.Runtime}}'
docker inspect ollama-embedding --format 'runtime={{.HostConfig.Runtime}}'
docker exec ollama-qa sh -lc 'ls /dev/nvhost-gpu /dev/nvmap /dev/nvhost-ctrl-gpu'
```

模板故意不包含 `build:` 段，也不引用 `wechatopenai/*` 上游镜像。WeKnora app、frontend、docreader 必须使用飞书发布表里的 SWR 镜像：

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

部署模板中不放 Dockerfile。Dockerfile 只属于构建流程，保留在源码构建目录并通过 [build-images.md](../build-images.md) 使用；部署目录只保留 compose、`.env` 和运行配置，避免误触发本机构建或上游镜像。

## 从飞书读取最新镜像版本

部署时不要猜 tag，也不要从相邻组件列抄 tag。飞书发布表是镜像版本来源：

```text
表格 token：Htotsn3oahO1zxt73YMcaB1zn8e
表格地址：https://*.feishu.cn/sheets/Htotsn3oahO1zxt73YMcaB1zn8e
```

按目标机器平台打开对应 sheet：

```text
AMD 机器：AMD_with_cuda 或 AMD_with_mxn100
ARM/L4T 机器：ARM_without_cuda、l4t、ARM_with_cuda、thor_spark、SOPHON_bm1688
```

在 sheet 中找这三列：

```text
weknora
weknora-ui
weknora-docreader
```

读取规则：

```text
第 1 行：服务名
第 2 行：镜像仓库地址
日期行：tag
完整镜像：<第 2 行仓库地址>:<日期行 tag>
```

优先选择最新日期行中三个服务列都不为空的一组 tag，然后写入部署目录 `.env`：

```env
WEKNORA_APP_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
WEKNORA_UI_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
WEKNORA_DOCREADER_IMAGE=swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

如果某个平台 sheet 没有这三列或最新日期行缺 tag，先按 [build-images.md](../build-images.md) 构建并推送，不要回退到 `wechatopenai/*` 或源码默认镜像。

也可以直接让部署脚本读取飞书并写 `.env`：

```bash
./deploy.sh --platform amd
./deploy.sh --platform l4t
./deploy.sh --platform thor
```

`deploy.sh` 默认使用 `~/.feishu.components.json`，没有时回退到 `~/.feishu.json`。脚本写入三个 WeKnora 镜像变量，拉取镜像并比较运行容器的 image digest，只替换发生变化的 frontend、app、docreader；Postgres、Redis、Neo4j 和模型后端不会被重启。app/docreader 或部署配置变化后，脚本运行 `trigger-reparse-incomplete.sh` 补交失败/未完成文档。

已有部署目录一键更新：

```bash
./update-and-deploy.sh --platform amd
./update-and-deploy.sh --platform l4t
./update-and-deploy.sh --platform thor
```

脚本从 `WEKNORA_DEPLOY_REPO`（默认 `git@github.com:ictrektech/WeKnora.git`）拉取 `WEKNORA_DEPLOY_REF`（默认 `main`），同步最新部署模板，同时保留 `.env*`、`data/`、日志和 `config/builtin_models*.yaml`，然后调用 `deploy.sh` 按需替换。

新部署：

```bash
mkdir -p /data/jhu/deploy/weknora
cp -R docs/ictrek/deploy-template/. /data/jhu/deploy/weknora/
cd /data/jhu/deploy/weknora
cp .env.example .env
mkdir -p data/files data/docreader data/postgres data/redis config
```

编辑 `.env`，至少改：

```text
WEKNORA_APP_IMAGE
WEKNORA_UI_IMAGE
WEKNORA_DOCREADER_IMAGE
DB_PASSWORD
REDIS_PASSWORD
FRONTEND_PORT
APP_PORT
```

确认不会拉上游镜像：

```bash
docker compose config | grep -E 'wechatopenai|swr.cn-southwest-2.myhuaweicloud.com/ictrek'
```

输出中不应出现 `wechatopenai`，必须出现 ictrek SWR 镜像。

启动：

```bash
docker compose pull frontend app docreader
docker compose up -d postgres redis docreader app frontend
```

Orin NX / L4T 纯 Ollama 部署：

```bash
cp .env.example .env
cp .env.orin-ollama.example .env.orin-ollama
# 把 .env.orin-ollama 中的镜像 tag、密码、端口和模型目录改成目标机器实际值。
set -a
. ./.env
. ./.env.orin-ollama
set +a
docker compose --env-file .env \
  -f docker-compose.yml \
  -f docker-compose.orin-ollama.yml up -d postgres redis docreader ollama-qa ollama-embedding app frontend
```

然后准备模型：

```bash
docker compose --env-file .env -f docker-compose.yml -f docker-compose.orin-ollama.yml \
  exec ollama-qa ollama pull "${OLLAMA_QA_MODEL:-qwen3.5:2b}"
docker compose --env-file .env -f docker-compose.yml -f docker-compose.orin-ollama.yml \
  exec ollama-embedding ollama pull "${OLLAMA_EMBEDDING_MODEL:-bge-m3:latest}"
```

如果要用 YAML 预置模型，复制分离 Ollama 示例：

```bash
cp config/builtin_models.orin-ollama.yaml.example config/builtin_models.yaml
```

并取消 `docker-compose.yml` 中这一行注释：

```yaml
- ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

分离 Ollama 时不要把三个模型都建成 `source=local`。`source=local` 只能共用一个 `OLLAMA_BASE_URL`，适合单 Ollama 容器；分离容器要用 `source=remote`，分别指向 `http://ollama-qa:11535/v1` 和 `http://ollama-embedding:11535/v1`。

启用 Neo4j/GraphRAG 时：

```bash
sed -i 's/^ENABLE_GRAPH_RAG=.*/ENABLE_GRAPH_RAG=true/' .env
sed -i 's/^NEO4J_ENABLE=.*/NEO4J_ENABLE=true/' .env
docker compose --profile neo4j up -d neo4j
docker compose up -d app
```

只有明确要 YAML 托管模型时，才复制并挂载：

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

然后取消 `docker-compose.yml` 中这一行注释：

```yaml
- ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

默认建议先启动服务，再在 Web UI 中添加模型，避免把某台机器的模型端口写死进镜像或模板。

GraphRAG 会调用同一个 LLM 后端做实体和关系抽取。为了避免图抽取把聊天模型占满，模板提供两个限流变量：

```text
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_ASYNQ_CONCURRENCY=2
WEKNORA_WIKI_ASYNQ_CONCURRENCY=2
WEKNORA_MODEL_MAX_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_WIKI_INGEST_MAP_PARALLEL=2
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=2
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_PARSE=5
WEKNORA_ASYNQ_QUEUE_MULTIMODAL=3
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=2
WEKNORA_REPARSE_WAIT_URLS=
WEKNORA_REPARSE_READY_WAIT_SECONDS=300
```

`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` 表示至少给聊天问答保留 2 路并发。解析池由 `WEKNORA_ASYNQ_CONCURRENCY` 控制，Wiki 独立池由 `WEKNORA_WIKI_ASYNQ_CONCURRENCY` 控制；`WEKNORA_MODEL_MAX_CONCURRENCY` 对同一模型的后台 Chat/VLM/Embedding 请求做统一上限。共享主模型时，这三个值都不应超过 `主模型并发 - 聊天预留`。

单文档 Graph 抽取的 LLM 并发由 `WEKNORA_GRAPH_LLM_CONCURRENCY` 控制，并且会被 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY/2` 限制。Wiki map/reduce 先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库没填时使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` / `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL`。小机器建议 env 默认设为 `1` 或 `2`。

部署模板默认设置 `WEKNORA_REPARSE_INCOMPLETE_ON_START=true`。app 重启后会先等待 `WEKNORA_REPARSE_WAIT_URLS` 中的模型服务 ready，再扫描 failed/pending/processing 文档；finalizing 只有在 `processed_at` 为空时才会整文档重新入队。启动扫描走 `critical` 队列，每条知识重新解析前会清掉该知识残留的 queued/retry 任务，再提交新的 `parse` 任务。旧 attempt 里还显示 running 的 trace 是被新 attempt 覆盖后的历史行，不要按旧 attempt 判断当前进度。需要手动补救时，也可以运行 [trigger-reparse-incomplete.sh](trigger-reparse-incomplete.sh)。文档页工具栏的「重新解析失败文档」只扫描当前知识库的 failed 文档；pending/processing 和 `processed_at` 为空的 finalizing 交给启动钩子或部署脚本处理。

Housekeeping 在 app 启动时立即执行一次，之后每 5 分钟运行。它会在最新 attempt 没有 `pending/running` span、Asynq 也没有该知识的 queued/active 任务时修复陈旧的 `pending_subtasks_count` 并推进 finalizing 文档；仍在排队或运行的任务不会被清理。启动恢复前还会删除旧 attempt 和完全重复的 Asynq 任务，日志可搜索 `startup-task-reconcile`。

Ollama 单实例只能用 `OLLAMA_NUM_PARALLEL` 控制整个服务的并发，不能给聊天和 embedding 分别硬预留槽位。Orin NX 16G 这类小机器推荐两个 Ollama 容器：QA/VLM 容器 `OLLAMA_CONTEXT_LENGTH=18000`、`OLLAMA_QA_NUM_PARALLEL=3`、`WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3`，并用 `WEKNORA_CHAT_RESERVED_CONCURRENCY=2` 保留聊天；embedding 容器 `OLLAMA_EMBEDDING_NUM_PARALLEL=4`，app 侧 `CONCURRENCY_POOL_SIZE=1`。QA 上下文需要大于 16k 时不要设成正好 `16384`；机器稳定且内存、等待队列都有余量后，再逐步提高 QA 并发。

更多并发、队列和模型服务容量检查见 [CONCURRENCY.md](CONCURRENCY.md)。文件上传默认限制为 `MAX_FILE_SIZE_MB=500`，修改后需要同时重启 frontend、app、docreader。

OpenAI-compatible 模型如果需要关闭 thinking，在 Web UI 的模型高级参数里把 `thinking_control` 设为后端支持的字段：`chat_template_kwargs`、`enable_thinking`、`thinking_type`、`think`、`reasoning_effort` 或 `none`。Ollama OpenAI-compatible 通常用 `think` 或 `reasoning_effort`。
