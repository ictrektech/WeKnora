# 本地开发复用 VOS Model Hub Ollama

**当前建议：已安装 V-Scheduler（统一模型网关与调度服务）时，改用 [`scheduler` 开发配置](README.md#v-scheduler)，统一访问宿主机 `22888/v1`。** 下文保留旧代理的兼容操作与历史验证；直接代理到 V-Scheduler 自带 Ollama 端口不能替代统一网关的资源准入。切换后完全重启开发后端，无需重建旧代理。

本文面向在宿主机运行 WeKnora Go/Vite 的开发者，说明如何复用已安装 VOS Model Hub 的目标主机上的 Ollama。目标是让本地开发程序访问 VOS Model Hub 的模型，复用 Model Hub 的预热、常驻和并发配置。已实现 ReRank（对检索结果重新评分和排序）的模型行及代理，并已在本机 Model Hub `0.0.92` 验证。已有两端口代理需重建以提供 `31537`；模型行变动后需重启后端。

历史验证（2026-10-21，本机 V-Scheduler `0.0.47`）：旧 `model-hub-ollama-*` 名称已无法解析，服务使用 `v-scheduler-ollama-*`。代理上游名称过期会导致文档向量生成等请求报 `EOF`（连接提前关闭）。本次切换统一网关后不再依赖这些代理名称。

## 结论与边界

| 项目 | 本地开发默认值（保留旧 Model Hub 名称） |
| --- | --- |
| Model Hub 网络 | `vos_default` |
| QA / 聊天 / VLM 上游 | `model-hub-ollama-qa:11535` |
| Embedding 上游 | `model-hub-ollama-embedding:11535` |
| ReRank 上游 | `model-hub-ollama-rerank:11434` |
| 开发机 QA 端口 | `127.0.0.1:31535` |
| 开发机 Embedding 端口 | `127.0.0.1:31536` |
| 开发机 ReRank 端口 | `127.0.0.1:31537` |
| 代理镜像 | `weknora-ictrek-model-hub-proxy:dev` |

本方案的链路如下：

```text
宿主机 WeKnora Go 进程
  -> 127.0.0.1:31535 / 127.0.0.1:31536 / 127.0.0.1:31537
  -> model-hub-gateway 代理容器
  -> vos_default Docker 网络
  -> QA/Embedding Gateway:11535 或 ReRank Ollama:11434
```

QA/Embedding 使用 `11535` Gateway（推理请求入口），保留该入口的槽位、阶段和速度统计。ReRank 与当前 VOS 安装包一致，使用 ictrek Ollama 的原生 `11434/v1/rerank`；此路径是否提供相同指标待验证。`35005` 是 Model Hub 后端管理 API，不是模型推理端口。Model Hub 的服务、模型预热和 Gateway 说明见 [VOS Model Hub 模型预热与常驻](../vos-ollama-prewarm.md)。

当前状态：

- 已验证（历史 QA/Embedding）：代理拓扑使用 `weknora-ictrek-model-hub-proxy:dev`，将 `31535/31536` 转发到 QA/Embedding 服务；目标主机需要先导入镜像后才能运行它。
- 已验证（本机模拟）：模型配置入库、ReRank 请求与响应转发、上游不可用时健康检查失败；见 `test_proxy.py` 和 `TestLocalDevModelHubRerank`。
- 已验证（本机 Model Hub `0.0.92`）：`31537` 转发到 Model Hub ReRank `11434`，真实 `/v1/rerank` 请求返回 `200` 且相关文档排序正确；模型行在 `jetson-model-hub` 为默认项，在 `tc232` 为非默认可选项。
- 已记录：下面的镜像导出、导入和 `docker run` 流程可以在另一台已安装 Model Hub 的 VOS 主机上重建同样的网络桥接。
- 已补齐运行管理：当前 `local-dev/docker-compose.yml` 仍不构建或声明这个代理，但 `ictrek-dev.sh start` 和 `start-model-hub-proxy` 会在镜像已构建或导入、上游已加入 `vos_default` 时创建或复用独立代理。代理源码和 Dockerfile 位于 `local-dev/model-hub-proxy/`。

## 上游名称变更

以下为旧代理的历史兼容配置；V-Scheduler 开发接入使用上面的 `scheduler` 配置。

当前主机的 V-Scheduler（管理模型服务的应用）在 `vos_default` 网络提供以下别名。在本地开发 `.env` 中设置已有脚本支持的覆盖值：

```env
ICTREK_DEV_MODEL_HUB_QA_UPSTREAM=v-scheduler-ollama-qa:11535
ICTREK_DEV_MODEL_HUB_EMBEDDING_UPSTREAM=v-scheduler-ollama-embedding:11535
ICTREK_DEV_MODEL_HUB_RERANK_UPSTREAM=v-scheduler-ollama-rerank:11434
```

按[创建代理容器](#创建代理容器)中的检查和重建步骤应用配置；仅重启旧容器不会更新其环境变量。宿主机 `31535`、`31536`、`31537` 端口不变，仍分别连接聊天、向量生成和重排服务。其他主机应先核对实际网络别名，继续使用旧 Model Hub 名称的主机不需要这些覆盖值。

旧名称无法解析时，代理的 `ProxyHandler.handle` 捕获上游连接错误后直接关闭客户端连接，因此后端只收到 `EOF`；代理健康检查同时失败。这种情况应先检查代理目标，不必修改文档内容。

已验证：通过重建后的三个代理端口，`qwen3.5:2b` 聊天返回 `200`，`bge-m3` 返回 `1024` 维向量，`qllama/bge-reranker-v2-m3:q8_0` 重排返回 `200` 且相关文本排在首位。`tc232` 的 `ictrek-weknora-ollama-embedding` 是本地 Ollama 备用模型，沿用全局 `OLLAMA_BASE_URL`，因此实际经 QA 端口 `31535` 生成向量；该路径也已验证。下文独立 Embedding 端口 `31536` 的配置约定适用于 `jetson-model-hub` 模型配置。

## 前置条件

已验证（2026-10-12）：本机已升级到 Model Hub `0.0.92`。开发代理已重建为三端口版本，`http://127.0.0.1:31537/v1/rerank` 返回 `200`。之前的 `connection refused` 来自旧代理缺少 `31537` 映射；升级 Model Hub 不会自动更新开发代理。

在目标 VOS 主机确认：

1. Model Hub 已通过 VOS 安装并运行；ReRank 需 `>= 0.0.92` 版本提供下述接口。
2. `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 已加入 `vos_default` 网络，并且对应模型已经下载或处于可按需启动状态。
3. `model-hub-ollama-rerank` 在 `vos_default` 网络运行，已下载 `qllama/bge-reranker-v2-m3:q8_0`，并提供 `/v1/models` 和 `/v1/rerank`。
4. 目标主机已有新版代理镜像 `weknora-ictrek-model-hub-proxy:dev`，或者可以在目标 Jetson 本地构建/从已有开发机导入该镜像。
5. 宿主机的 `31535`、`31536` 和 `31537` 端口没有被其他程序占用。

检查网络和 Model Hub 容器：

```bash
VOS_NETWORK=vos_default

docker network inspect "$VOS_NETWORK" >/dev/null
docker ps --format '{{.Names}}\t{{.Networks}}' \
  | rg 'model-hub-ollama-(qa|embedding|rerank)'
```

在 Model Hub 容器内验证 Gateway：

```bash
QA_CONTAINER="$(docker ps -q --filter 'name=model-hub-ollama-qa' | head -n 1)"
EMBEDDING_CONTAINER="$(docker ps -q --filter 'name=model-hub-ollama-embedding' | head -n 1)"
RERANK_CONTAINER="$(docker ps -q --filter 'name=model-hub-ollama-rerank' | head -n 1)"

docker exec "$QA_CONTAINER" \
  wget -qO- http://127.0.0.1:11535/v1/models
docker exec "$EMBEDDING_CONTAINER" \
  wget -qO- http://127.0.0.1:11535/v1/models
docker exec "$RERANK_CONTAINER" \
  wget -qO- http://127.0.0.1:11434/v1/models
```

如果容器没有运行、Gateway 不可用或模型列表为空，先在 VOS Model Hub 管理页面启动对应服务和模型，再继续下面的步骤。

## 构建代理镜像

代理只依赖 Python 标准库，没有 GPU 或第三方 Python 依赖；目标 Jetson 可直接构建 ARM64 镜像：

```bash
docker build --platform linux/arm64 \
  -t weknora-ictrek-model-hub-proxy:dev \
  -f ictrek.app/docs/local-dev/model-hub-proxy/Dockerfile \
  ictrek.app/docs/local-dev/model-hub-proxy
```

## 导出和导入代理镜像

如果目标主机不方便构建，也可以从已有开发机导出已经验证的镜像，再导入目标 VOS 主机。注意 AMD64 镜像不能直接在 Jetson ARM64 上使用。

在已有代理镜像的机器上执行：

```bash
PROXY_IMAGE=weknora-ictrek-model-hub-proxy:dev
docker image inspect "$PROXY_IMAGE" >/dev/null
docker save "$PROXY_IMAGE" | gzip > /tmp/weknora-ictrek-model-hub-proxy-dev.tar.gz
```

将 `/tmp/weknora-ictrek-model-hub-proxy-dev.tar.gz` 复制到目标主机后执行：

```bash
gunzip -c /tmp/weknora-ictrek-model-hub-proxy-dev.tar.gz | docker load
docker image inspect weknora-ictrek-model-hub-proxy:dev
```

镜像需要与目标主机的 CPU 架构匹配。代理只转发 TCP/HTTP 流量，不需要 GPU。

## 创建代理容器

代理容器只需要加入 `vos_default` 网络；宿主机运行的 Go 进程通过发布到 `127.0.0.1` 的端口访问它，不需要加入 Docker 网络。

该代理应作为独立容器管理。当前 `local-dev/docker-compose.yml` 不声明这个服务，因此不要依赖本地开发 Compose 自动拉起或重建它。

在目标 VOS 主机执行：

```bash
PROXY_NAME=weknora-ictrek-local-dev-model-hub-gateway
PROXY_IMAGE=weknora-ictrek-model-hub-proxy:dev
VOS_NETWORK=vos_default
QA_HOST_PORT=31535
EMBEDDING_HOST_PORT=31536
RERANK_HOST_PORT=31537

docker run -d \
  --name "$PROXY_NAME" \
  --restart unless-stopped \
  --network "$VOS_NETWORK" \
  -p "127.0.0.1:${QA_HOST_PORT}:11535" \
  -p "127.0.0.1:${EMBEDDING_HOST_PORT}:11536" \
  -p "127.0.0.1:${RERANK_HOST_PORT}:11537" \
  -e MODEL_HUB_QA_UPSTREAM=model-hub-ollama-qa:11535 \
  -e MODEL_HUB_EMBEDDING_UPSTREAM=model-hub-ollama-embedding:11535 \
  -e MODEL_HUB_RERANK_UPSTREAM=model-hub-ollama-rerank:11434 \
  -e MODEL_HUB_QA_LISTEN_PORT=11535 \
  -e MODEL_HUB_EMBEDDING_LISTEN_PORT=11536 \
  -e MODEL_HUB_RERANK_LISTEN_PORT=11537 \
  --entrypoint python3 \
  "$PROXY_IMAGE" /app/proxy.py
```

不要在未确认容器名称的情况下执行删除命令。如果目标主机已经存在同名代理，先检查其配置：

```bash
docker inspect "$PROXY_NAME" \
  --format '{{json .Config.Env}} {{json .HostConfig.PortBindings}} {{json .NetworkSettings.Networks}}'
```

旧代理只有两个监听端口，单纯 `restart-model-hub-proxy` 不会更新容器配置。先确认上面的检查结果对应本地开发代理；重建镜像后，移除这个无持久化数据的代理容器，再执行 `start-model-hub-proxy`。此操作会中断正在通过代理进行的请求，不涉及 Model Hub 容器：

```bash
docker rm -f "$PROXY_NAME"
./ictrek.app/docs/local-dev/ictrek-dev.sh start-model-hub-proxy
```

代理创建后确认它同时能看到 Model Hub 服务和宿主机端口：

```bash
docker logs "$PROXY_NAME"
curl -fsS "http://127.0.0.1:${QA_HOST_PORT}/v1/models"
curl -fsS "http://127.0.0.1:${EMBEDDING_HOST_PORT}/v1/models"
curl -fsS "http://127.0.0.1:${RERANK_HOST_PORT}/v1/models"
```

## 配置本地 WeKnora

在运行本地 Go 后端的开发环境 `.env` 中设置 QA Ollama Gateway：

```env
ICTREK_DEV_OLLAMA_BASE_URL=http://127.0.0.1:31535
OLLAMA_BASE_URL=http://127.0.0.1:31535
```

使用 local-dev helper 时，`setup --profile jetson-model-hub` 会写入上述固定代理地址；`start-model-hub-proxy` 或后续 `start` 会负责创建/复用独立代理容器。Embedding 模型行始终使用 `source: remote` 和 `http://127.0.0.1:31536/v1`，不要把它改成全局 `OLLAMA_BASE_URL`。

`tc232` 也可手动执行 `start-model-hub-proxy`，其模型列表包含同一 ReRank 可选行，默认仍为 vLLM。仅显示模型行不代表上游可用；本地模式不会自动从 Model Hub 下载模型。

然后重启本地后端（已有 `tc232` 配置执行 `setup` 会保留该 profile）：

```bash
./ictrek.app/docs/local-dev/ictrek-dev.sh setup
./ictrek.app/docs/local-dev/ictrek-dev.sh app
```

模型配置与端点的关系如下：

| 模型行 | 推荐配置 | 地址 |
| --- | --- | --- |
| QA / VLM，直接使用本地 Ollama 适配器 | `source: local`，`base_url: ""` | 使用全局 `OLLAMA_BASE_URL`，即 `31535` |
| QA / VLM，使用 OpenAI-compatible 适配器 | `source: remote`，`interface_type: openai` | `http://127.0.0.1:31535/v1` |
| Embedding | `source: remote`，`interface_type: openai` | `http://127.0.0.1:31536/v1` |
| ReRank | `source: remote`，`provider: generic`，`score_scale: probability`（`0～1` 相关度） | `http://127.0.0.1:31537/v1` |

当前 `builtin_models.tc232.yaml` 中的 Ollama QA/VLM 行是非默认的 fallback 行；选择这些行后，QA/VLM 会通过 `OLLAMA_BASE_URL` 访问代理。该文件的默认 QA、VLM 和 Embedding 仍然是 vLLM 端点，不会因为启动代理而自动切换。

如果需要让 Model Hub 模型成为默认模型，建议在本地开发数据库使用单独的 YAML，至少将模型行配置为：

```yaml
# QA / VLM
source: remote
parameters:
  base_url: http://127.0.0.1:31535/v1
  interface_type: openai
  provider: generic

# Embedding
source: remote
parameters:
  base_url: http://127.0.0.1:31536/v1
  interface_type: openai
  provider: generic
```

模型名必须与目标 Model Hub 的 `/v1/models` 返回值一致，本 profile 默认使用 `qwen3.5:2b`、`bge-m3:latest` 和 `qllama/bge-reranker-v2-m3:q8_0`。不要把本地开发 YAML 指向 VOS 生产数据库；YAML 托管行可能会被同步或软删除。

后端启动会把 YAML（模型配置文件）的 ReRank 行同步到本地开发数据库。ReRank 的模型名和地址使用 `${NAME:-default}`（环境变量缺失或为空时使用默认值）：旧 `.env` 无需新增变量也会显示 `qllama/bge-reranker-v2-m3:q8_0`，默认地址为 `http://127.0.0.1:31537/v1`；非空环境变量仍优先。未声明默认值的 `${NAME}` 继续保留缺失变量的占位符，便于发现配置问题。

重启会修正先前写入的 YAML 托管异常模型名、展示名和地址；通过管理界面修改过的运行时模型仍保留人工设置。重启后应在「模型管理」和智能体 ReRank 下拉框看到该模型；已有智能体不会因此自动切换模型，需手动选择并保存。

## 端到端验证

本机实测覆盖两条中文候选文档的接口调用，以及 HybRAG 聊天重排阶段（`outcome=ok`）。未覆盖浏览器完整会话和 Model Hub 调用指标。

先验证模型列表：

```bash
curl -fsS http://127.0.0.1:31535/v1/models
curl -fsS http://127.0.0.1:31536/v1/models
curl -fsS http://127.0.0.1:31537/v1/models
```

再分别测试 QA、Embedding 和 ReRank：

```bash
curl -N -fsS http://127.0.0.1:31535/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen3.5:2b",
    "messages": [{"role": "user", "content": "回复：Model Hub OK"}],
    "stream": true,
    "max_tokens": 64
  }'

curl -fsS http://127.0.0.1:31536/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "bge-m3",
    "input": ["Model Hub embedding 测试"]
  }'

curl -fsS http://127.0.0.1:31537/v1/rerank \
  -H 'Content-Type: application/json' \
  -d '{"model":"qllama/bge-reranker-v2-m3:q8_0","query":"知识库检索","documents":["知识库检索流程","今天的天气"]}'
```

同时查看 Model Hub QA 或 Embedding 容器日志，确认请求进入的是 `11535` Gateway，而不是原生 `11434`：

```bash
docker logs --since 5m "$(docker ps -q --filter 'name=model-hub-ollama-qa' | head -n 1)"
docker logs --since 5m "$(docker ps -q --filter 'name=model-hub-ollama-embedding' | head -n 1)"
```

## 开发机与 VOS 主机不在同一台机器

上面的 `127.0.0.1` 端口只对运行代理的 VOS 主机可见。开发机在另一台机器时，优先使用 SSH 隧道，不要直接把未认证的模型 Gateway 暴露到公网：

```bash
ssh -N \
  -L 31535:127.0.0.1:31535 \
  -L 31536:127.0.0.1:31536 \
  -L 31537:127.0.0.1:31537 \
  <vos-user>@<vos-host>
```

SSH 隧道建立后，开发机 `.env` 仍使用：

```env
OLLAMA_BASE_URL=http://127.0.0.1:31535
```

只有在已配置私网访问控制、防火墙和认证措施时，才考虑把 Docker 发布地址从 `127.0.0.1` 改为指定的私网地址。

## 故障排查

- `model-hub-ollama-qa`、`model-hub-ollama-embedding` 或 `model-hub-ollama-rerank` 解析失败：确认代理加入的是目标 VOS 的 `vos_default` 网络，并检查服务 alias。
- 宿主机 `31535/31536/31537` 连接被拒绝：检查代理容器状态、端口映射和 `docker logs`。
- `/v1/models` 有响应但模型调用报模型不存在：检查 Model Hub 管理页面的模型名，以及请求中的 `model` 是否完全一致。
- Embedding 调用到了 QA 服务：不要使用 `source: local` 的本地 Embedding fallback；使用 `source: remote` 并把 `base_url` 配置为 `http://127.0.0.1:31536/v1`。
- QA/Embedding 在 Model Hub 看不到槽位或 token/s：确认调用地址使用 `11535` Gateway。ReRank 的指标能力待 VOS 联调确认。
- ReRank 下拉框没有模型或显示 `${ICTREK_DEV_MODEL_HUB_RERANK_MODEL_NAME}`：更新代码后完全重启 `$DEV app`/Air，再刷新页面；仅刷新浏览器不会同步 YAML。若该模型已经过人工编辑，需在模型管理中修正名称和地址。
- ReRank 请求失败：检查 `31537/v1/models`、`31537/v1/rerank`，确认 Model Hub 版本、模型下载状态和名称；代理不会把失败请求转到 QA 或 vLLM。
- 端口冲突：修改 `QA_HOST_PORT`、`EMBEDDING_HOST_PORT`、`RERANK_HOST_PORT`，并同步修改本地 `.env` 和模型 YAML 的地址。
