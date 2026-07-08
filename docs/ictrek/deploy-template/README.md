# ictrek WeKnora 部署模板

本目录是 ictrek 远程部署模板。本文只保留中文说明。

这些文件用于部署已发布镜像：

```text
docker-compose.yml
.env.example
config/builtin_models.yaml.example
```

Orin NX / L4T 机器如果要用纯 Ollama 后端，额外使用：

```text
docker-compose.orin-ollama.yml
.env.orin-ollama.example
config/builtin_models.orin-ollama.yaml.example
```

这个 overlay 会启动两个 Ollama 容器：`ollama-qa` 只服务聊天和图片理解，`ollama-embedding` 只服务 embedding。这样文档向量化不会把聊天/VLM 的 Ollama 调度槽位吃满。

模板故意不包含 `build:` 段，也不引用 `wechatopenai/*` 上游镜像。WeKnora app、frontend、docreader 必须使用飞书发布表里的 SWR 镜像：

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

部署模板中不放 Dockerfile。Dockerfile 只属于构建流程，保留在源码构建目录并通过 [build-images.md](../build-images.md) 使用；部署目录只保留 compose、`.env` 和运行配置，避免误触发本机构建或上游镜像。

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
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_WIKI_INGEST_MAP_PARALLEL=2
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=2
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=1
```

`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` 表示后台图谱、问题生成、wiki 生成最多只能使用剩余模型槽位，至少给聊天问答保留 2 路并发。`WEKNORA_ASYNQ_QUEUE_CRITICAL` 保持最高权重，后台图谱/问题队列保持低权重。

单文档 Graph 抽取的 LLM 并发由 `WEKNORA_GRAPH_LLM_CONCURRENCY` 控制，并且会被 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY/2` 限制。Wiki map/reduce 先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库没填时使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` / `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL`。小机器建议 env 默认设为 `1` 或 `2`。

Ollama 单实例只能用 `OLLAMA_NUM_PARALLEL` 控制整个服务的并发，不能给聊天和 embedding 分别硬预留槽位。Orin NX 这类小机器推荐两个 Ollama 容器：QA/VLM 容器 `OLLAMA_QA_NUM_PARALLEL=4` 且 `WEKNORA_CHAT_RESERVED_CONCURRENCY=2`；embedding 容器 `OLLAMA_EMBEDDING_NUM_PARALLEL=4` 且 app 侧 `CONCURRENCY_POOL_SIZE=2`。如果机器稳定且显存有余，再把 QA 调到 `5`、聊天保留调到 `3`。

更多并发、队列和模型服务容量检查见 [CONCURRENCY.md](CONCURRENCY.md)。文件上传默认限制为 `MAX_FILE_SIZE_MB=500`，修改后需要同时重启 frontend、app、docreader。

OpenAI-compatible 模型如果需要关闭 thinking，在 Web UI 的模型高级参数里把 `thinking_control` 设为后端支持的字段：`chat_template_kwargs`、`enable_thinking`、`thinking_type`、`think`、`reasoning_effort` 或 `none`。Ollama OpenAI-compatible 通常用 `think` 或 `reasoning_effort`。
