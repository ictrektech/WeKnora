# 空机器 WeKnora 部署总指南

这是一份从空机器启动 WeKnora 的运维检查清单。中文说明在上方，英文原文在下方。更细的镜像构建、模型后端、Neo4j、上游同步等内容分别引用本目录中的专题文档。

`tc232` 这类名字只是操作员本机的 SSH config alias，不是服务 hostname。外部访问需要单独做公网端口映射、反向代理、VPN 或隧道。

## 给初次部署者的执行顺序

不要先随手 `docker compose up -d`。按下面顺序做，每一步通过后再继续：

1. 准备 Docker、数据目录和 `.env`。
2. 复制 ictrek 部署模板，并固定使用同一个部署目录。
3. 从飞书发布表选定已发布的 WeKnora 三个镜像，并写入 `.env`。
4. 启动并测试模型后端，例如 Ollama 或 vLLM。
5. 启动 postgres、redis、docreader。
6. 启动 app、frontend。
7. 登录 Web UI 添加模型，或确认挂载的 `config/builtin_models.yaml` 已生效。
8. 上传一个小文档，问一个能命中文档的问题。
9. 再问“你是谁”，确认没有旧品牌 prompt 或模板文本泄露。

其中第 2 步最容易造成数据看起来“丢失”：同一套部署只能长期使用同一组 compose 文件和同一套 Postgres 存储。升级、重启、排障时也必须使用同一组文件。

构建目录和部署目录必须分开。`/data/jhu/build/weknora` 这类目录只用于同步源码和执行 `build_image.sh`；不要在其中执行 `docker compose pull/up/restart`。运行服务必须在真实部署目录中操作。已有容器可用下面命令确认真实目录：

```bash
docker inspect <app-container> --format \
  'project={{index .Config.Labels "com.docker.compose.project"}} workdir={{index .Config.Labels "com.docker.compose.project.working_dir"}} config={{index .Config.Labels "com.docker.compose.project.config_files"}}'
```

如果在源码构建目录误跑 compose，Compose 会读取默认 `docker-compose.yml`，可能拉取上游 `wechatopenai/weknora-app:latest` 或触发本机构建，而不是使用 `swr.cn-southwest-2.myhuaweicloud.com/ictrek/...` 发布镜像。

## 目标服务

最小可用 WeKnora 部署包含：

```text
frontend   Web UI
app        WeKnora API 和 worker
docreader  文档解析服务
postgres   元数据；RETRIEVE_DRIVER=postgres 时也存向量数据
redis      stream/task 状态
```

基础 RAG 部署使用非 lite 栈，并保持：

```env
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files
```

可选组件按功能开启：

```text
neo4j      实体关系 GraphRAG
minio      用对象存储替代本地文件
qdrant     外部向量库
milvus     外部向量库
weaviate   外部向量库
searxng    自建网络搜索
langfuse   模型调用观测
```

## 机器准备

先确认 Docker 和 Compose：

```bash
docker version
docker compose version
```

如果本机要运行 GPU 模型后端，先验证 GPU runtime：

```bash
nvidia-smi
docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

Jetson/L4T 主机要使用对应平台的 NVIDIA container runtime 测试镜像。WeKnora 本身不依赖 GPU，但本地 vLLM/Ollama 模型后端要用 GPU 加速时，必须先解决 runtime。

从 ictrek 部署模板创建部署目录和持久化目录：

```bash
mkdir -p /data/jhu/deploy/weknora
cp -R docs/ictrek/deploy-template/. /data/jhu/deploy/weknora/
cd /data/jhu/deploy/weknora
cp .env.example .env
mkdir -p data/files data/docreader data/postgres data/redis config
```

`/data/jhu` 不是强制路径，可以换成本机实际数据盘。不要从源码根目录复制默认 `docker-compose.yml`；部署目录应使用 `docs/ictrek/deploy-template/docker-compose.yml`。

## 选择 WeKnora 镜像

如果飞书发布表里已有镜像，优先使用已有镜像：

- 表格 token：`Htotsn3oahO1zxt73YMcaB1zn8e`；
- AMD 机器看 `AMD_with_cuda` 或 `AMD_with_mxn100`；
- ARM/L4T 机器看 `ARM_without_cuda`、`l4t`、`ARM_with_cuda`、`thor_spark`、`SOPHON_bm1688`；
- 在目标平台 sheet 中找 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox`；
- 第 1 行是服务名，第 2 行是仓库地址，日期行是 tag；
- 优先选最新日期行中四个服务列都不为空的一组 tag；
- 组合成 `<第 2 行仓库地址>:<日期行 tag>`；
- 写入部署目录 `.env` 的 `WEKNORA_APP_IMAGE`、`WEKNORA_UI_IMAGE`、`WEKNORA_DOCREADER_IMAGE`、`WEKNORA_SANDBOX_DOCKER_IMAGE`。

发布镜像不包含部署专用模型行。模型后续在 Web UI 添加，或者由运维人员显式挂载 `config/builtin_models.yaml`。

如果目标平台没有可用镜像，先暂停部署，按 [build-images.md](build-images.md) 完成构建、推送和飞书记录后再回来继续。WeKnora app/frontend/docreader/sandbox 镜像本身没有 CUDA 依赖，tag 不应带 CUDA 标记。

## 准备模型后端

至少需要：

```text
KnowledgeQA  聊天/问答模型
Embedding    向量模型
```

需要图片/文档图片描述时，还要：

```text
VLLM         视觉模型
```

Rerank 可选。只有真实 `/v1/rerank` 或等价 rerank endpoint 可用时才配置。

### 方案 A：Ollama 为主

Ollama 可以同时提供聊天、图片理解和 embedding。小机器上要先区分两种部署方式：

```text
单 Ollama 容器
  简单，但 OLLAMA_NUM_PARALLEL 是全局并发，聊天、VLM、embedding 会互相抢槽位。

QA/VLM 和 Embedding 分离为两个 Ollama 容器
  推荐给 Orin NX / L4T 这类空机器。聊天和图片理解走 ollama-qa，
  embedding 走 ollama-embedding，文档入库不会占用聊天容器槽位。
```

具体容器启动和常驻模型方式见 [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)。

#### 单 Ollama 容器

WeKnora 启动前先准备模型：

```bash
ollama pull qwen3.5:2b
ollama pull bge-m3
```

WeKnora 模型行建议：

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

`source=local` 的 Ollama 行不要填 `base_url` 和 `api_key`，WeKnora 会统一使用 `.env` 里的 `OLLAMA_BASE_URL`。

单 Ollama 容器的并发只能尽量保守。QA 上下文需要大于 16k 时不要设成正好 `16384`，VOS 普通 profile 默认用 `24000`：

```env
OLLAMA_CONTEXT_LENGTH=24000
OLLAMA_NUM_PARALLEL=3
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
CONCURRENCY_POOL_SIZE=2
BATCH_EMBED_SIZE=4
```

这只能保证后台 LLM 调用不吃掉全部 QA 槽位，不能阻止 embedding 请求在 Ollama 内部和聊天请求排队。

#### Orin NX / L4T 分离 Ollama 容器

空的 Orin NX 机器优先使用部署模板中的 overlay：

```bash
cd /data/jhu/deploy/weknora
cp .env.example .env
cp .env.orin-ollama.example .env.orin-ollama
```

编辑 `.env` 和 `.env.orin-ollama`，至少改：

```text
WEKNORA_APP_IMAGE / WEKNORA_UI_IMAGE / WEKNORA_DOCREADER_IMAGE / WEKNORA_SANDBOX_DOCKER_IMAGE
DB_PASSWORD / REDIS_PASSWORD / JWT_SECRET / TENANT_AES_KEY / SYSTEM_AES_KEY
OLLAMA_SERVER_IMAGE
OLLAMA_QA_MODELS_DIR
OLLAMA_EMBEDDING_MODELS_DIR
OLLAMA_QA_MODEL
OLLAMA_EMBEDDING_MODEL
```

启动：

```bash
set -a
. ./.env
. ./.env.orin-ollama
set +a

docker compose --env-file .env \
  -f docker-compose.yml \
  -f docker-compose.orin-ollama.yml \
  up -d postgres redis docreader ollama-qa ollama-embedding app frontend
```

拉取模型并 warmup：

```bash
docker compose --env-file .env -f docker-compose.yml -f docker-compose.orin-ollama.yml \
  exec ollama-qa ollama pull "${OLLAMA_QA_MODEL:-qwen3.5:2b}"
docker compose --env-file .env -f docker-compose.yml -f docker-compose.orin-ollama.yml \
  exec ollama-embedding ollama pull "${OLLAMA_EMBEDDING_MODEL:-bge-m3:latest}"

curl -fsS http://127.0.0.1:${OLLAMA_QA_GATEWAY_HOST_PORT:-21535}/v1/models
curl -fsS http://127.0.0.1:${OLLAMA_EMBEDDING_GATEWAY_HOST_PORT:-21536}/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'
```

分离 Ollama 时，模型行不要用 `source=local`，因为 `source=local` 只能共用一个 `OLLAMA_BASE_URL`。用 `source=remote` 指向两个 gateway：

```text
KnowledgeQA  source=remote  name=qwen3.5:2b   base_url=http://ollama-qa:11535/v1
VLLM         source=remote  name=qwen3.5:2b   base_url=http://ollama-qa:11535/v1
Embedding    source=remote  name=bge-m3:latest base_url=http://ollama-embedding:11535/v1 dimension=1024
```

如果要通过 YAML 预置模型：

```bash
cp config/builtin_models.orin-ollama.yaml.example config/builtin_models.yaml
```

然后取消 `docker-compose.yml` 中这一行注释：

```yaml
- ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

推荐起步并发：

```env
OLLAMA_CONTEXT_LENGTH=24000
OLLAMA_QA_NUM_PARALLEL=8
OLLAMA_EMBEDDING_NUM_PARALLEL=4
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=1
WEKNORA_WIKI_INGEST_MAP_PARALLEL=1
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=1
WEKNORA_ASYNQ_CORE_CONCURRENCY=1
WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY=1
WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY=1
WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY=1
WEKNORA_ASYNQ_SHARED_CONCURRENCY=1
WEKNORA_WIKI_ASYNQ_CONCURRENCY=1
WEKNORA_MODEL_MAX_CONCURRENCY=1
CONCURRENCY_POOL_SIZE=2
BATCH_EMBED_SIZE=4
```

机器稳定且内存、等待队列都有余量时，再逐步提高 `OLLAMA_QA_NUM_PARALLEL`。先不要提高 embedding、Graph 或 Wiki 并发。

原生 Ollama 不提供通用 rerank API。Ollama 主方案如果也要 rerank，需要另准备 rerank 模型或 sidecar，并通过 gateway 暴露 `/v1/rerank`；否则不要配置 rerank。

### 方案 B：vLLM 做聊天/VLM，Ollama 做 embedding

聊天和 VLM 走 OpenAI-compatible vLLM endpoint，embedding 走 Ollama。参考：

- [remote-vllm-backend.md](remote-vllm-backend.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)

模型行形态：

```text
KnowledgeQA  source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
VLLM         source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
Embedding    source=local 或 remote，取决于使用 Ollama 原生 API 还是 OpenAI-compatible embedding gateway
```

如果 app 容器通过宿主机映射端口访问模型服务，`SSRF_WHITELIST_EXTRA` 中要保留 `host.docker.internal`。

## `.env` 要改什么

从 `docs/ictrek/deploy-template/.env.example` 开始：

```bash
cp .env.example .env
```

ictrek 模板是单文件部署 compose，不需要 `COMPOSE_FILE` 拼接。后续所有 `docker compose` 命令都在同一个部署目录执行。不要把源码根目录的 `docker-compose.yml`、`docker-compose.override.yml`、`docker-compose.images.yml` 混进这套模板。

升级已有服务时，不要凭记忆 `cd` 到某个目录。先用 `docker inspect` 读取正在运行 app 容器的 `com.docker.compose.project.working_dir` 和 `com.docker.compose.project.config_files`，再在该目录执行 `docker compose pull/up/restart`。执行前确认 `docker compose config | grep 'swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora'` 能看到 ictrek 镜像。

基础项：

```env
GIN_MODE=release
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN
DISABLE_REGISTRATION=false
# create_personal：注册后自动创建个人空间；tenantless：只创建账户。
WEKNORA_AUTH_DEFAULT_TENANT_MODE=create_personal
# false 时普通用户只能通过邀请加入既有空间。
WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED=true

DB_USER=postgres
DB_PASSWORD=<strong-password>
DB_NAME=WeKnora

REDIS_PASSWORD=<strong-password>
REDIS_DB=0
REDIS_PREFIX=stream:

RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files

FRONTEND_PORT=19080
APP_PORT=19081
DOCREADER_PORT=50051
MAX_FILE_SIZE_MB=500

JWT_SECRET=<strong-random-value>
TENANT_AES_KEY=<strong-random-value>
SYSTEM_AES_KEY=<32-byte-value>
```

聊天问答优先级：

```env
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_ASYNQ_CORE_CONCURRENCY=1
WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY=1
WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY=1
WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY=1
WEKNORA_ASYNQ_SHARED_CONCURRENCY=1
WEKNORA_WIKI_ASYNQ_CONCURRENCY=1
WEKNORA_MODEL_MAX_CONCURRENCY=6
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_CHAT_MODEL_CONTEXT_TOKENS=24000
WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS=0
WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS=8000
WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS=8000
```

含义：后台图谱抽取、问题生成、Wiki 生成最多使用剩余模型槽位，至少给聊天问答保留 2 路并发。worker 改为独立的 core、postprocess、enrichment、maintenance、shared、wiki 池；模板全部从 `1` 起步。旧的 `WEKNORA_ASYNQ_CONCURRENCY` 和 `WEKNORA_ASYNQ_QUEUE_*` 不再生效。

单文档 Graph 抽取由 `WEKNORA_GRAPH_LLM_CONCURRENCY` 控制，并会被主 QA 并发的一半限制。Wiki map/reduce 先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库没填时使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` / `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL`。Orin NX 建议 env 默认设为 `1`，个别大知识库再单独调高。

最终答案合成由 `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS`、`WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS`、`WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS` 共同控制。最终输出 token 太大时会被代码按上下文窗口自动夹紧，并至少保留 512 token 给输入上下文；不要用超大输出预算掩盖模型上下文过小的问题。`WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS` 控制普通知识库问答摘要/回答生成的输出 token。

更完整的并发、队列和模型服务容量检查见 [deploy-template/CONCURRENCY.md](deploy-template/CONCURRENCY.md)。

Ollama 主方案：

```env
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

vLLM 通过宿主机映射端口访问时，同样保留 `host.docker.internal` 白名单，并在模型行中使用 `http://host.docker.internal:<vllm-port>/v1`。

Neo4j/GraphRAG：

```env
NEO4J_ENABLE=true
ENABLE_GRAPH_RAG=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=<strong-password>
```

参考 [neo4j.env.example](neo4j.env.example)。

## 模型用 UI 还是 YAML

首次部署推荐：先启动 WeKnora，登录后在 Web UI 中添加模型行。这样不会把某台机器的模型 ID、端口、后端写死进镜像或 compose。

只有需要声明式下发模型时，才使用 `config/builtin_models.yaml`：

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

然后在 `docs/ictrek/deploy-template/docker-compose.yml` 复制出来的部署文件中取消挂载注释：

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

YAML 中常改字段：

```yaml
builtin_models:
  - id: <stable-id-used-by-kb-and-agents>
    type: KnowledgeQA | VLLM | Embedding | Rerank
    source: local | remote
    is_default: true
    name: <model-name-or-ollama-tag>
    parameters:
      base_url: <empty-for-local-ollama-or-remote-/v1-url>
      api_key: <empty-or-env-placeholder>
      provider: generic
      embedding_parameters:
        dimension: <embedding-dimension>
```

Ollama `source=local` 行保持 `base_url`、`api_key` 为空，在 `.env` 配 `OLLAMA_BASE_URL`。vLLM/OpenAI-compatible 行使用 `source=remote`，`base_url` 以 `/v1` 结尾。

生命周期规则：

```text
builtin_models.yaml 创建的行会标记为 managed_by='yaml'。
app 启动时，当前 YAML 中不存在的 YAML 托管行会被软删除。
Web UI/API/手工 SQL 创建的行应保持 managed_by=''，不会被 YAML 清理。
```

把 YAML 改成空 `builtin_models: []` 前，先确认知识库、智能体和 GraphRAG 没有引用即将被软删除的模型行。排查方式见 [remote-weknora-deployment.md](remote-weknora-deployment.md#model-row-troubleshooting)。

## docker compose 用哪个

生产或远程空机器上，优先使用 `docs/ictrek/deploy-template/docker-compose.yml`：

```bash
docker compose pull frontend app docreader
docker compose up -d postgres redis docreader app frontend
```

执行前检查：

```bash
docker compose config | grep -E 'wechatopenai|swr.cn-southwest-2.myhuaweicloud.com/ictrek'
```

输出中不应出现 `wechatopenai`，必须出现 ictrek SWR 镜像。

### 持久化和 compose 文件必须固定

同一套部署从第一次启动开始，就必须一直使用这份模板复制出来的 `docker-compose.yml`。不要一会儿用模板目录，一会儿又去源码根目录执行 compose。

模板已经把本地持久化写进同一个 compose 文件：

```text
./data/files      -> app:/data/files
./data/docreader  -> docreader:/tmp/docreader
./data/postgres   -> postgres:/var/lib/postgresql/data
./data/redis      -> redis:/data
```

升级镜像也在同一部署目录中执行：

```bash
docker compose pull docreader app frontend
docker compose up -d docreader app frontend
```

启动或升级后先确认实际挂载：

```bash
docker compose exec postgres sh -lc 'echo "$PGDATA"'
docker inspect <postgres-container-name> --format '{{json .Mounts}}'
docker inspect <app-container-name> --format '{{json .Mounts}}'
```

确认输出中包含预期宿主机路径，例如：

```text
./data/postgres  -> /var/lib/postgresql/data
./data/files     -> /data/files
./data/redis     -> /data
```

如果发现服务连到了 named volume，不要直接删容器或卷。先停止 app，确认旧数据目录和当前卷的数据量，再决定是切回模板部署目录，还是做数据库 dump/restore。

源码根目录没有 `docker-compose.images.yml` 时，Compose 会使用源码默认 image/build 配置。生产类环境不要在源码根目录启动服务。

可选 profile 按需启动：

```bash
docker compose --profile neo4j up -d neo4j
```

ictrek 模板目前只保留常用的 `neo4j` profile。MinIO、Qdrant、Milvus、Weaviate 等外部组件需要时单独按对应组件文档部署，不放进默认模板。

## 启动顺序

1. 启动模型后端并验证 API。
2. 按需启动 Neo4j、MinIO、外部向量库等可选服务。
3. 启动 WeKnora 依赖：

```bash
docker compose up -d postgres redis docreader
```

4. 启动 app 和 frontend：

```bash
docker compose up -d app frontend
```

5. 打开前端，注册或登录，添加模型行。默认 `create_personal` 会为新注册用户创建个人空间。若此实例只允许受邀用户访问，在首次对外开放前把 `.env` 改为 `DISABLE_REGISTRATION=true`、`WEKNORA_AUTH_DEFAULT_TENANT_MODE=tenantless`、`WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED=false`，然后执行 `docker compose up -d app`。`tenantless` 用户会进入创建或加入空间引导页；已有用户和空间不受影响。

VOS app 安装场景可以启用临时 iframe 免登录：

```env
HYBRAG_VOS_SSO_ENABLED=true
HYBRAG_VOS_USERINFO_URL=http://172.17.0.1:8105/v1000/user/check
```

这不是长期身份方案，只是兼容当前 VOS。前端会读取 VOS 同源会话 token，后端通过 `/v1000/user/check` 校验；首次打开会自动创建 `username@local` 用户和个人空间，`admin` 会成为 `admin@local` 系统管理员。未来 VOS 支持标准 OIDC 或 iframe 注入用户信息后，关闭 `HYBRAG_VOS_SSO_ENABLED` 或替换身份适配即可，HybRAG 本地用户和空间创建逻辑不需要改。

只建议对外暴露 frontend。app、docreader、数据库、Redis、Neo4j 默认应保持私有，除非明确为了调试而开放。

## 组件测试

容器和日志：

```bash
docker compose ps
docker compose logs --tail=200 app
docker compose logs --tail=100 docreader
```

App 健康：

```bash
curl -fsS http://127.0.0.1:<app-host-port>/health
```

Frontend：

```bash
curl -I http://127.0.0.1:<frontend-host-port>/
```

Postgres：

```bash
docker compose exec postgres pg_isready -U "$DB_USER"
```

Redis：

```bash
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" ping
```

docreader：

```bash
docker compose exec docreader grpc_health_probe -addr=localhost:50051
```

Ollama：

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-host-port>/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文知识库检索测试"]}'
```

vLLM：

```bash
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/models
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"<served-model>","messages":[{"role":"user","content":"用一句中文说明你是谁。"}],"max_tokens":128}'
```

Neo4j/APOC：

```bash
docker compose exec neo4j cypher-shell \
  -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" \
  'RETURN apoc.version();'
```

模型行：

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
order by type,id;"
```

持久化挂载：

```bash
docker inspect <postgres-container-name> --format '{{json .Mounts}}'
docker inspect <app-container-name> --format '{{json .Mounts}}'
```

如果模型列表、对话记录或知识库突然变少，先检查 Postgres 挂载是否仍是预期目录/卷，再检查 `models`、`sessions`、`messages` 表：

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select 'models' as table_name, count(*) from models
union all select 'sessions', count(*) from sessions
union all select 'messages', count(*) from messages
union all select 'knowledge_bases', count(*) from knowledge_bases;"
```

## 强制冒烟检查

`/health` 只能说明 app 进程还活着，不能证明模型、prompt、SSRF 白名单、RAG 链路正常。每次新部署、升级、重启模型后都要做：

1. 在 Web UI 里确认 KnowledgeQA、Embedding、可选 VLLM 模型都存在且没有被删除。
2. 问“你是谁”。正常回答应自称 `Vivibit AI 小助手`，不能出现 `WeKnora`、`Tencent`、`{{language}}`、`CRITICAL: Language Rule`。
3. 上传一个很小的 TXT/PDF，等解析成功后问文档里的明确事实。
4. 如果使用宿主机端口访问 vLLM/Ollama，确认没有：

```text
baseURL SSRF check failed
hostname host.docker.internal is restricted
```

5. 如果开启 GraphRAG，用短句做实体关系抽取，失败时先查 Neo4j/APOC，再查选中的 KnowledgeQA 模型行。

排查命令：

```bash
docker compose exec app env | grep SSRF
docker compose logs --tail=300 app | grep -E 'SSRF|model not found|fallback|CRITICAL|host.docker.internal|实体关系'
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,managed_by,deleted_at
from models
order by type,id;"
```

## 功能测试

在 Web UI 中完成：

1. 添加或选择默认 KnowledgeQA、Embedding，以及可选 VLLM 模型。
2. 创建知识库。
3. 上传小 TXT/PDF，确认解析成功。
4. 对上传内容提问，确认问答能命中文档。
5. 配置 VLM 时，上传或解析带图片的文档，确认图片描述不报错。
6. 开启 GraphRAG 时，用短文本做实体关系抽取，确认能返回节点和关系。

Wiki graph 和 Neo4j GraphRAG 不是一回事。Wiki graph 来自 WeKnora 内部 wiki 页面和链接；Neo4j 用于实体关系 GraphRAG。

## 出问题先看哪里

先看：

```bash
docker compose ps
docker compose logs --tail=300 app
docker compose logs --tail=200 docreader
docker compose logs --tail=200 postgres
docker compose logs --tail=200 redis
```

常见现象：

```text
前端能打开但 API 失败
  查 APP_HOST、APP_BACKEND_PORT、frontend 日志、app /health。

注册/登录正常但聊天失败
  查 KnowledgeQA 模型行、模型后端 /models 或 /api/tags、app 日志。

用户无法创建空间或登录后进入空间引导页
  查 DISABLE_REGISTRATION、WEKNORA_AUTH_DEFAULT_TENANT_MODE、WEKNORA_TENANT_SELF_SERVICE_CREATION_ENABLED，
  以及系统设置中保存的 auth.default_tenant_mode / tenant.self_service_creation_enabled（页面值优先于 env）。

管理员重置密码后用户被登出
  这是预期安全行为：重置会撤销该用户的全部既有会话，用户必须用新密码重新登录。

文档解析失败
  查 docreader health/logs、MAX_FILE_SIZE_MB、./data/files 挂载、app 日志。

embedding/indexing 失败
  查 Embedding 模型行、dimension、OLLAMA_BASE_URL 或 embedding base_url、embedding API。

baseURL SSRF check failed
  只把需要的模型 host 加入 SSRF_WHITELIST_EXTRA 或 SSRF_WHITELIST。
  容器访问宿主机映射模型后端时，保留 host.docker.internal。

GraphRAG 显示“实体关系提取失败”
  查 NEO4J_ENABLE、ENABLE_GRAPH_RAG、Neo4j/APOC、选中的聊天模型行。
  如果日志有 model not found，查 models 表的 managed_by 和 deleted_at。

YAML 模型行重启后消失
  该行是 managed_by='yaml'，但不在当前 builtin_models.yaml 中。
  把它放回 YAML，或重建/转换为 managed_by=''。

模型、对话记录、知识库突然变少
  优先查是否从源码目录或其他目录误跑了 docker compose。
  再查 Postgres 实际 Mounts，确认仍指向模板部署目录下的 ./data/postgres。
```

详细参考：

- [remote-weknora-deployment.md](remote-weknora-deployment.md)
- [build-images.md](build-images.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)
- [remote-vllm-backend.md](remote-vllm-backend.md)
- [neo4j.env.example](neo4j.env.example)
- [../BUILTIN_MODELS.md](../BUILTIN_MODELS.md)

---

# Fresh Host WeKnora Deployment

This note is the operator checklist for bringing up WeKnora on a machine that
has no existing WeKnora runtime state. It assumes the WeKnora images have
already been published and links to narrower ictrek notes for model backends and
feature-specific details.

`tc232` and similar names are SSH config aliases on an operator workstation.
They are not service hostnames. Any public access URL must be provided by a
separate port mapping, reverse proxy, VPN, or tunnel.

## Beginner Runbook

Do not start with an ad hoc `docker compose up -d`. Follow this order and only
continue after each step passes:

1. Prepare Docker, data directories, and `.env`.
2. Copy the ictrek deployment template and keep using the same deployment directory.
3. Pick the four released WeKnora images from the Feishu table and write them to `.env`.
4. Start and test model backends such as Ollama or vLLM.
5. Start postgres, redis, and docreader.
6. Start app and frontend.
7. Add models in the Web UI, or confirm the mounted `config/builtin_models.yaml`
   has been applied.
8. Upload a small document and ask a question that should hit that document.
9. Ask "你是谁" and confirm the answer does not leak old branding or template
   text.

Step 2 is the most common cause of apparently missing data. A deployment must
keep using the same compose file set and the same Postgres storage. Use that
same file set for upgrades, restarts, and troubleshooting.

Keep build and deployment directories separate. A directory such as
`/data/jhu/build/weknora` is only for source sync and `build_image.sh`; do not
run `docker compose pull/up/restart` there. Runtime operations must happen in
the real deployment directory. For an existing container, confirm it with:

```bash
docker inspect <app-container> --format \
  'project={{index .Config.Labels "com.docker.compose.project"}} workdir={{index .Config.Labels "com.docker.compose.project.working_dir"}} config={{index .Config.Labels "com.docker.compose.project.config_files"}}'
```

If compose is run from the source build directory by mistake, Docker Compose
reads the default `docker-compose.yml` and may pull upstream
`wechatopenai/weknora-app:latest` or build locally instead of using the
`swr.cn-southwest-2.myhuaweicloud.com/ictrek/...` release images.

## Target Shape

The smallest useful deployment runs:

```text
frontend   Web UI
app        WeKnora API and workers
docreader  document parsing service
postgres   metadata, vector data when RETRIEVE_DRIVER=postgres
redis      stream/task state
```

Use the default non-lite stack. For baseline RAG, keep:

```env
RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files
```

Add optional services only when the feature is used:

```text
neo4j      entity/relation GraphRAG
minio      object storage instead of local files
qdrant     external vector database
milvus     external vector database
weaviate   external vector database
searxng    self-hosted web search
langfuse   model-call observability
```

## Machine Prerequisites

Install and verify:

```bash
docker version
docker compose version
```

For GPU model backends, verify the platform-specific runtime before starting
vLLM or Ollama:

```bash
nvidia-smi
docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

On Jetson/L4T hosts, use the host's NVIDIA container runtime test image instead
of the CUDA x86 image. Fix GPU runtime first; WeKnora can run without GPU, but
local model backends cannot use acceleration without it.

Create a deployment directory from the ictrek deployment template:

```bash
mkdir -p /data/jhu/deploy/weknora
cp -R docs/ictrek/deploy-template/. /data/jhu/deploy/weknora/
cd /data/jhu/deploy/weknora
cp .env.example .env
mkdir -p data/files data/docreader data/postgres data/redis config
```

Use your own root path if `/data/jhu` is not appropriate. Do not copy the source
root `docker-compose.yml`; the deployment directory should use
`docs/ictrek/deploy-template/docker-compose.yml`.

## Pick WeKnora Images

If images already exist, prefer the released-image path:

- read the platform sheet in the Feishu release table;
- find the `weknora`, `weknora-ui`, `weknora-docreader`, and `weknora-sandbox` columns;
- combine row 2 repository URI with the selected dated row tag;
- write the resulting images to `WEKNORA_APP_IMAGE`, `WEKNORA_UI_IMAGE`, and
  `WEKNORA_DOCREADER_IMAGE`, and `WEKNORA_SANDBOX_DOCKER_IMAGE` in the deployment `.env`.

The released WeKnora images do not include deployment-specific model rows.
That is intentional. Add models later in the UI, or mount an operator-created
`config/builtin_models.yaml`.

If images do not exist for the platform, stop the deployment first, complete the
build, push, and Feishu release table update through [build-images.md](build-images.md),
then return here. Do not include CUDA markers in WeKnora image tags unless the
WeKnora image itself starts depending on CUDA libraries.

## Prepare Model Backends

WeKnora needs at least:

```text
chat model       KnowledgeQA
embedding model  Embedding
```

For image/document description, also configure:

```text
vision model     VLLM
```

Rerank is optional. Only configure it when a real rerank endpoint exists.

### Option A: Ollama-First

Use this when one local Ollama service should provide chat, image understanding,
and embedding. Follow
[model-hub-ollama-embedding.md](model-hub-ollama-embedding.md) for the exact
container command and keep-alive behavior.

Prepare the Ollama service and models before starting WeKnora:

```bash
ollama pull qwen3.5:2b
ollama pull bge-m3
```

Expected WeKnora model rows:

```text
KnowledgeQA  source=local  name=qwen3.5:2b
VLLM         source=local  name=qwen3.5:2b
Embedding    source=local  name=bge-m3:latest  dimension=1024
```

For local Ollama rows, leave `base_url` and `api_key` empty. WeKnora uses
`OLLAMA_BASE_URL` for all local Ollama calls.

Plain Ollama does not provide a generic rerank API. To use rerank in an
Ollama-first deployment, prepare a rerank-capable model or sidecar plus a
gateway that exposes `/v1/rerank`; otherwise leave rerank unconfigured.

### Option B: vLLM For Chat/VLM, Ollama For Embedding

Use this when the chat/VLM model should run behind an OpenAI-compatible vLLM
endpoint and embeddings should run through Ollama. Follow:

- [remote-vllm-backend.md](remote-vllm-backend.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)

Expected WeKnora model rows:

```text
KnowledgeQA  source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
VLLM         source=remote  name=<vllm-served-model>  base_url=http://host.docker.internal:<vllm-port>/v1
Embedding    source=local or remote, depending on whether native Ollama or an OpenAI-compatible embedding gateway is used
```

Keep `host.docker.internal` in `SSRF_WHITELIST_EXTRA` when the app container
calls model services through host-mapped ports.

## Prepare `.env`

Start from `.env.example`:

```bash
cp .env.example .env
```

The ictrek template is a single deployment compose file, so it does not need
`COMPOSE_FILE` composition. Run every `docker compose` command from the same
deployment directory. Do not mix the source root `docker-compose.yml`,
`docker-compose.override.yml`, or `docker-compose.images.yml` into this template.

For existing services, do not `cd` by memory before an upgrade. Read the running
app container labels `com.docker.compose.project.working_dir` and
`com.docker.compose.project.config_files`, then run `docker compose
pull/up/restart` from that directory. Before executing, verify that `docker
compose config | grep 'swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora'`
shows the ictrek images.

Set the baseline values:

```env
GIN_MODE=release
TZ=Asia/Shanghai
WEKNORA_LANGUAGE=zh-CN

DB_USER=postgres
DB_PASSWORD=<strong-password>
DB_NAME=WeKnora

REDIS_PASSWORD=<strong-password>
REDIS_DB=0
REDIS_PREFIX=stream:

RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
STREAM_MANAGER_TYPE=redis
LOCAL_STORAGE_BASE_DIR=/data/files

FRONTEND_PORT=19080
APP_PORT=19081
DOCREADER_PORT=50051
MAX_FILE_SIZE_MB=500

JWT_SECRET=<strong-random-value>
TENANT_AES_KEY=<strong-random-value>
SYSTEM_AES_KEY=<32-byte-value>
```

Chat/QA priority:

```env
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_ASYNQ_CORE_CONCURRENCY=1
WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY=1
WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY=1
WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY=1
WEKNORA_ASYNQ_SHARED_CONCURRENCY=1
WEKNORA_WIKI_ASYNQ_CONCURRENCY=1
WEKNORA_MODEL_MAX_CONCURRENCY=6
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_CHAT_MODEL_CONTEXT_TOKENS=24000
WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS=0
WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS=8000
WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS=8000
```

后台图谱抽取、问题生成和 Wiki 生成使用剩余模型槽位，至少保留两路给聊天问答。worker 使用独立 core、postprocess、enrichment、maintenance、shared、wiki 池；模板全部从 `1` 起步。旧的 `WEKNORA_ASYNQ_CONCURRENCY` 和 `WEKNORA_ASYNQ_QUEUE_*` 不再生效。

See [deploy-template/CONCURRENCY.md](deploy-template/CONCURRENCY.md) for full
concurrency, queue, and model backend capacity checks.

For Ollama-first:

```env
OLLAMA_BASE_URL=http://host.docker.internal:<ollama-host-port>
SSRF_WHITELIST_EXTRA=host.docker.internal,searxng,qdrant,milvus,weaviate,doris-fe
```

For vLLM through a host-mapped port, keep the same `SSRF_WHITELIST_EXTRA` and
use `http://host.docker.internal:<vllm-port>/v1` in the model row.

For Neo4j/GraphRAG:

```env
NEO4J_ENABLE=true
ENABLE_GRAPH_RAG=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=<strong-password>
```

See [neo4j.env.example](neo4j.env.example).

## Decide How To Add Models

Recommended for first deployments: start WeKnora, log in, and add model rows in
the Web UI. This avoids stale model IDs baked into images or compose files.

Use `config/builtin_models.yaml` only when the deployment needs declarative
model rows. In that case:

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

Edit only the model entries needed by the deployment, then uncomment the model
mount in the copied `docs/ictrek/deploy-template/docker-compose.yml`:

```yaml
services:
  app:
    volumes:
      - ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

For each YAML model entry, the fields that normally change per deployment are:

```yaml
builtin_models:
  - id: <stable-id-used-by-kb-and-agents>
    type: KnowledgeQA | VLLM | Embedding | Rerank
    source: local | remote
    is_default: true
    name: <model-name-or-ollama-tag>
    parameters:
      base_url: <empty-for-local-ollama-or-remote-/v1-url>
      api_key: <empty-or-env-placeholder>
      provider: generic
      embedding_parameters:
        dimension: <embedding-dimension>
```

For Ollama `source=local` rows, keep `base_url` and `api_key` empty and set
`OLLAMA_BASE_URL` in `.env`. For vLLM/OpenAI-compatible rows, use
`source=remote` and set `base_url` to the endpoint ending in `/v1`.

Important lifecycle rule:

```text
Rows created from builtin_models.yaml are tagged managed_by='yaml'.
On app startup, YAML-managed rows missing from the current YAML are soft-deleted.
Rows created in the UI/API/manual SQL should keep managed_by='' and are not touched.
```

Before switching to an empty `builtin_models: []`, make sure knowledge bases,
agents, and GraphRAG do not still reference model rows that are about to be
soft-deleted. See [remote-weknora-deployment.md](remote-weknora-deployment.md#model-row-troubleshooting).

## Compose Files

Use `docs/ictrek/deploy-template/docker-compose.yml` for a normal released-image
deployment:

```bash
docker compose pull frontend app docreader
docker compose up -d postgres redis docreader app frontend
```

Before starting, check:

```bash
docker compose config | grep -E 'wechatopenai|swr.cn-southwest-2.myhuaweicloud.com/ictrek'
```

The output should not contain `wechatopenai`; it must contain ictrek SWR images.

### Keep Persistence And Compose Files Stable

Use this copied template `docker-compose.yml` from the first startup onward. Do
not alternate between the template directory and the source root directory.

The template keeps local persistence in the same compose file:

```text
./data/files      -> app:/data/files
./data/docreader  -> docreader:/tmp/docreader
./data/postgres   -> postgres:/var/lib/postgresql/data
./data/redis      -> redis:/data
```

Use the same deployment directory for image upgrades:

```bash
docker compose pull docreader app frontend
docker compose up -d docreader app frontend
```

After startup or upgrade, verify the real mounts:

```bash
docker compose exec postgres sh -lc 'echo "$PGDATA"'
docker inspect <postgres-container-name> --format '{{json .Mounts}}'
docker inspect <app-container-name> --format '{{json .Mounts}}'
```

The output should include the intended host paths:

```text
./data/postgres  -> /var/lib/postgresql/data
./data/files     -> /data/files
./data/redis     -> /data
```

If a service is attached to an unexpected named volume, do not delete
containers or volumes first. Stop the app, compare the old data directory and
current volume, then either restart from the template deployment directory or do an
explicit dump/restore.

If compose is run from the source root, Docker Compose can fall back to the
source default image names and build sections. Do not start production-like
services from the source root.

Start optional profiles only when needed:

```bash
docker compose --profile neo4j up -d neo4j
```

The ictrek template currently keeps only the common `neo4j` profile. Deploy
MinIO, Qdrant, Milvus, Weaviate, and similar external services separately when
needed.

## Startup Order

1. Start model backends and verify their APIs.
2. Start optional storage/graph services such as Neo4j if needed.
3. Start WeKnora dependencies:

```bash
docker compose up -d postgres redis docreader
```

4. Start app and frontend:

```bash
docker compose up -d app frontend
```

5. Open the frontend host/port, register or log in, and add model rows if they
were not declared through YAML.

Only expose the frontend publicly. Keep app, docreader, database, Redis, and
Neo4j ports private unless an operator intentionally publishes them for
debugging.

## Component Checks

Check containers:

```bash
docker compose ps
docker compose logs --tail=200 app
docker compose logs --tail=100 docreader
```

Check app health:

```bash
curl -fsS http://127.0.0.1:<app-host-port>/health
```

Check frontend:

```bash
curl -I http://127.0.0.1:<frontend-host-port>/
```

Check Postgres:

```bash
docker compose exec postgres pg_isready -U "$DB_USER"
```

Check Redis:

```bash
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" ping
```

Check docreader:

```bash
docker compose exec docreader grpc_health_probe -addr=localhost:50051
```

Check Ollama:

```bash
curl -fsS http://127.0.0.1:<ollama-host-port>/api/tags
curl -fsS http://127.0.0.1:<ollama-host-port>/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文知识库检索测试"]}'
```

Check vLLM:

```bash
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/models
curl -fsS http://127.0.0.1:<vllm-host-port>/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"<served-model>","messages":[{"role":"user","content":"用一句中文说明你是谁。"}],"max_tokens":128}'
```

Check Neo4j and APOC:

```bash
docker compose exec neo4j cypher-shell \
  -u "$NEO4J_USERNAME" -p "$NEO4J_PASSWORD" \
  'RETURN apoc.version();'
```

Check model rows:

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,is_builtin,managed_by,deleted_at
from models
order by type,id;"
```

Check persistence mounts:

```bash
docker inspect <postgres-container-name> --format '{{json .Mounts}}'
docker inspect <app-container-name> --format '{{json .Mounts}}'
```

If models, chat history, or knowledge bases suddenly look much smaller, check
the Postgres mount first, then inspect the core table counts:

```bash
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select 'models' as table_name, count(*) from models
union all select 'sessions', count(*) from sessions
union all select 'messages', count(*) from messages
union all select 'knowledge_bases', count(*) from knowledge_bases;"
```

## Mandatory Smoke Check

`/health` only proves that the app process is alive. It does not prove that
model backends, prompts, SSRF allowlists, or the RAG path work. After every
fresh deployment, upgrade, or model backend restart:

1. In the Web UI, confirm KnowledgeQA, Embedding, and optional VLLM model rows
   exist and are not deleted.
2. Ask "你是谁". The answer should identify as `Vivibit AI 小助手` and must not
   contain `WeKnora`, `Tencent`, `{{language}}`, or `CRITICAL: Language Rule`.
3. Upload a small TXT/PDF and ask a question with an answer in that document.
4. If vLLM/Ollama is reached through host-mapped ports, confirm these errors do
   not appear:

```text
baseURL SSRF check failed
hostname host.docker.internal is restricted
```

5. If GraphRAG is enabled, run entity/relation extraction on a short sentence.
   On failure, check Neo4j/APOC first, then the selected KnowledgeQA model row.

Useful commands:

```bash
docker compose exec app env | grep SSRF
docker compose logs --tail=300 app | grep -E 'SSRF|model not found|fallback|CRITICAL|host.docker.internal|实体关系'
docker compose exec postgres psql -U "$DB_USER" -d "$DB_NAME" -c "
select id,type,source,name,is_default,managed_by,deleted_at
from models
order by type,id;"
```

## Functional Checks

Use the Web UI for the final checks:

1. Create or select default KnowledgeQA, Embedding, and optional VLLM model rows.
2. Create a knowledge base.
3. Upload a small TXT/PDF file and confirm parsing succeeds.
4. Ask a question that should hit the uploaded content.
5. If VLM is configured, upload or parse a document containing an image and
   confirm image description does not fail.
6. If GraphRAG is enabled, run entity/relation extraction on a short text and
   confirm nodes/relations are returned.

The Wiki graph page is separate from Neo4j GraphRAG. Wiki graph is generated
from wiki pages and links inside WeKnora; Neo4j is used for entity/relation
GraphRAG.

## Troubleshooting Map

Use these first:

```bash
docker compose ps
docker compose logs --tail=300 app
docker compose logs --tail=200 docreader
docker compose logs --tail=200 postgres
docker compose logs --tail=200 redis
```

Common symptoms:

```text
Frontend opens but API fails
  Check APP_HOST, APP_BACKEND_PORT, frontend logs, and app /health.

Register/login works but chat fails
  Check KnowledgeQA model row, model backend /models or /api/tags, and app logs.

Document parsing fails
  Check docreader health/logs, MAX_FILE_SIZE_MB, mounted ./data/files, and app logs.

Embedding/indexing fails
  Check Embedding row, dimension, OLLAMA_BASE_URL or embedding base_url, and embedding API.

baseURL SSRF check failed
  Add only the required model host to SSRF_WHITELIST_EXTRA or SSRF_WHITELIST.
  For host-mapped local backends, keep host.docker.internal whitelisted.

GraphRAG shows “实体关系提取失败”
  Check NEO4J_ENABLE, ENABLE_GRAPH_RAG, Neo4j/APOC, and the selected chat model row.
  If logs contain model not found, inspect managed_by and deleted_at in models.

YAML model rows disappear after restart
  The row was managed_by='yaml' and absent from the current builtin_models.yaml.
  Put it back in YAML, or recreate/convert it to managed_by=''.

Models, chat history, or knowledge bases suddenly look smaller
  First check whether docker compose was run from the source root or another
  wrong directory.
  Then inspect the real Postgres mounts and confirm it still points to
  ./data/postgres under the template deployment directory.
```

Detailed references:

- [remote-weknora-deployment.md](remote-weknora-deployment.md)
- [build-images.md](build-images.md)
- [model-hub-ollama-embedding.md](model-hub-ollama-embedding.md)
- [remote-vllm-backend.md](remote-vllm-backend.md)
- [neo4j.env.example](neo4j.env.example)
- [../BUILTIN_MODELS.md](../BUILTIN_MODELS.md)
