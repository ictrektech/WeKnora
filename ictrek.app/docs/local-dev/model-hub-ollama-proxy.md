# 本地开发复用 VOS Model Hub Ollama

本文面向在宿主机运行 WeKnora Go/Vite 的开发者，说明如何复用已安装 VOS Model Hub 的目标主机上的 Ollama。目标是让本地开发程序访问 VOS Model Hub 的模型，同时保留 Model Hub 的预热、常驻、并发槽位和调用指标。

## 结论与边界

| 项目 | 当前约定 |
| --- | --- |
| Model Hub 网络 | `vos_default` |
| QA / 聊天 / VLM 上游 | `model-hub-ollama-qa:11535` |
| Embedding 上游 | `model-hub-ollama-embedding:11535` |
| 开发机 QA 端口 | `127.0.0.1:31535` |
| 开发机 Embedding 端口 | `127.0.0.1:31536` |
| 代理镜像 | `weknora-ictrek-model-hub-proxy:dev` |

本方案的链路如下：

```text
宿主机 WeKnora Go 进程
  -> 127.0.0.1:31535 / 127.0.0.1:31536
  -> model-hub-gateway 代理容器
  -> vos_default Docker 网络
  -> Model Hub Ollama Gateway:11535
```

`11535` 是 Model Hub 的 Gateway，必须优先使用；不要改成 Ollama 原生的 `11434`，否则请求不会进入 Model Hub 的槽位、阶段和速度统计。`35005` 是 Model Hub 后端管理 API，不是模型推理端口。Model Hub 的服务、模型预热和 Gateway 说明见 [VOS Model Hub 模型预热与常驻](../vos-ollama-prewarm.md)。

当前状态：

- 已验证：代理拓扑使用 `weknora-ictrek-model-hub-proxy:dev`，将 `31535/31536` 转发到上面的两个 Model Hub 服务；目标主机需要先导入镜像后才能运行它。
- 已记录：下面的镜像导出、导入和 `docker run` 流程可以在另一台已安装 Model Hub 的 VOS 主机上重建同样的网络桥接。
- 已补齐运行管理：当前 `local-dev/docker-compose.yml` 仍不构建或声明这个代理，但 `ictrek-dev.sh start` 和 `start-model-hub-proxy` 会在镜像已构建或导入、上游已加入 `vos_default` 时创建或复用独立代理。代理源码和 Dockerfile 位于 `local-dev/model-hub-proxy/`。

## 前置条件

在目标 VOS 主机确认：

1. Model Hub 已通过 VOS 安装并运行。
2. `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 已加入 `vos_default` 网络，并且对应模型已经下载或处于可按需启动状态。
3. 目标主机已有代理镜像 `weknora-ictrek-model-hub-proxy:dev`，或者可以在目标 Jetson 本地构建/从已有开发机导入该镜像。
4. 宿主机的 `31535` 和 `31536` 端口没有被其他程序占用。

检查网络和 Model Hub 容器：

```bash
VOS_NETWORK=vos_default

docker network inspect "$VOS_NETWORK" >/dev/null
docker ps --format '{{.Names}}\t{{.Networks}}' \
  | rg 'model-hub-ollama-(qa|embedding)'
```

在 Model Hub 容器内验证 Gateway：

```bash
QA_CONTAINER="$(docker ps -q --filter 'name=model-hub-ollama-qa' | head -n 1)"
EMBEDDING_CONTAINER="$(docker ps -q --filter 'name=model-hub-ollama-embedding' | head -n 1)"

docker exec "$QA_CONTAINER" \
  wget -qO- http://127.0.0.1:11535/v1/models
docker exec "$EMBEDDING_CONTAINER" \
  wget -qO- http://127.0.0.1:11535/v1/models
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

docker run -d \
  --name "$PROXY_NAME" \
  --restart unless-stopped \
  --network "$VOS_NETWORK" \
  -p "127.0.0.1:${QA_HOST_PORT}:11535" \
  -p "127.0.0.1:${EMBEDDING_HOST_PORT}:11536" \
  -e MODEL_HUB_QA_UPSTREAM=model-hub-ollama-qa:11535 \
  -e MODEL_HUB_EMBEDDING_UPSTREAM=model-hub-ollama-embedding:11535 \
  -e MODEL_HUB_QA_LISTEN_PORT=11535 \
  -e MODEL_HUB_EMBEDDING_LISTEN_PORT=11536 \
  --entrypoint python3 \
  "$PROXY_IMAGE" /app/proxy.py
```

不要在未确认容器名称的情况下执行删除命令。如果目标主机已经存在同名代理，先检查其配置：

```bash
docker inspect "$PROXY_NAME" \
  --format '{{json .Config.Env}} {{json .HostConfig.PortBindings}} {{json .NetworkSettings.Networks}}'
```

代理创建后确认它同时能看到 Model Hub 服务和宿主机端口：

```bash
docker logs "$PROXY_NAME"
curl -fsS "http://127.0.0.1:${QA_HOST_PORT}/v1/models"
curl -fsS "http://127.0.0.1:${EMBEDDING_HOST_PORT}/v1/models"
```

## 配置本地 WeKnora

在运行本地 Go 后端的开发环境 `.env` 中设置 QA Ollama Gateway：

```env
ICTREK_DEV_OLLAMA_BASE_URL=http://127.0.0.1:31535
OLLAMA_BASE_URL=http://127.0.0.1:31535
```

使用 local-dev helper 时，`setup --profile jetson-model-hub` 会写入上述固定代理地址；`start-model-hub-proxy` 或后续 `start` 会负责创建/复用独立代理容器。Embedding 模型行始终使用 `source: remote` 和 `http://127.0.0.1:31536/v1`，不要把它改成全局 `OLLAMA_BASE_URL`。

然后重启本地后端：

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

模型名必须与目标 Model Hub 的 `/v1/models` 返回值一致，通常分别是 `qwen3.5:2b` 和 `bge-m3`。不要把本地开发 YAML 指向 VOS 生产数据库；YAML 托管行可能会被同步或软删除。

当前代理只提供 QA 和 Embedding 两个监听端口。ReRank 仍使用现有 vLLM 或其他独立端点；如果要复用 `model-hub-ollama-rerank:11535`，需要为代理增加第三个监听端口和对应的镜像实现。

## 端到端验证

先验证模型列表：

```bash
curl -fsS http://127.0.0.1:31535/v1/models
curl -fsS http://127.0.0.1:31536/v1/models
```

再分别测试 QA 和 Embedding：

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
  <vos-user>@<vos-host>
```

SSH 隧道建立后，开发机 `.env` 仍使用：

```env
OLLAMA_BASE_URL=http://127.0.0.1:31535
```

只有在已配置私网访问控制、防火墙和认证措施时，才考虑把 Docker 发布地址从 `127.0.0.1` 改为指定的私网地址。

## 故障排查

- `model-hub-ollama-qa` 或 `model-hub-ollama-embedding` 解析失败：确认代理加入的是目标 VOS 的 `vos_default` 网络，并检查服务 alias。
- 宿主机 `31535/31536` 连接被拒绝：检查代理容器状态、端口映射和 `docker logs`。
- `/v1/models` 有响应但模型调用报模型不存在：检查 Model Hub 管理页面的模型名，以及请求中的 `model` 是否完全一致。
- Embedding 调用到了 QA 服务：不要使用 `source: local` 的本地 Embedding fallback；使用 `source: remote` 并把 `base_url` 配置为 `http://127.0.0.1:31536/v1`。
- Model Hub 看不到槽位或 token/s：确认调用地址使用 `11535` Gateway，不要直连 `11434`。
- 端口冲突：修改 `QA_HOST_PORT`、`EMBEDDING_HOST_PORT`，并同步修改本地 `.env` 和模型 YAML 的地址。
