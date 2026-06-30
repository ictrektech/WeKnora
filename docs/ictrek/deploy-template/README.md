# ictrek WeKnora 部署模板

本目录是 ictrek 远程部署模板。中文说明在上方，英文原文在下方。

这些文件用于部署已发布镜像：

```text
docker-compose.yml
.env.example
config/builtin_models.yaml.example
```

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

---

# ictrek WeKnora Deployment Template

This directory is the ictrek remote deployment template.

It is for running released images:

```text
docker-compose.yml
.env.example
config/builtin_models.yaml.example
```

The template intentionally has no `build:` sections and does not reference
upstream `wechatopenai/*` images. WeKnora app, frontend, and docreader must use
SWR images from the Feishu release table:

```text
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-ui:<tag>
swr.cn-southwest-2.myhuaweicloud.com/ictrek/weknora-docreader:<tag>
```

Dockerfiles are intentionally not copied into this deployment template. They
belong to the build flow documented in [build-images.md](../build-images.md).
The deployment directory should only contain compose, `.env`, and runtime
configuration so it cannot accidentally build locally or fall back to upstream
images.

For a fresh deployment:

```bash
mkdir -p /data/jhu/deploy/weknora
cp -R docs/ictrek/deploy-template/. /data/jhu/deploy/weknora/
cd /data/jhu/deploy/weknora
cp .env.example .env
mkdir -p data/files data/docreader data/postgres data/redis config
```

Edit `.env`, at least:

```text
WEKNORA_APP_IMAGE
WEKNORA_UI_IMAGE
WEKNORA_DOCREADER_IMAGE
DB_PASSWORD
REDIS_PASSWORD
FRONTEND_PORT
APP_PORT
```

Confirm it cannot pull upstream images:

```bash
docker compose config | grep -E 'wechatopenai|swr.cn-southwest-2.myhuaweicloud.com/ictrek'
```

The output should not contain `wechatopenai`; it must contain ictrek SWR images.

Start:

```bash
docker compose pull frontend app docreader
docker compose up -d postgres redis docreader app frontend
```

For Neo4j/GraphRAG:

```bash
sed -i 's/^ENABLE_GRAPH_RAG=.*/ENABLE_GRAPH_RAG=true/' .env
sed -i 's/^NEO4J_ENABLE=.*/NEO4J_ENABLE=true/' .env
docker compose --profile neo4j up -d neo4j
docker compose up -d app
```

Only copy and mount YAML-managed models when the deployment intentionally uses
declarative models:

```bash
cp config/builtin_models.yaml.example config/builtin_models.yaml
```

Then uncomment this line in `docker-compose.yml`:

```yaml
- ./config/builtin_models.yaml:/app/config/builtin_models.yaml:ro
```

The default recommendation is to start the stack first and add models in the Web
UI, so machine-specific model ports are not baked into images or templates.
