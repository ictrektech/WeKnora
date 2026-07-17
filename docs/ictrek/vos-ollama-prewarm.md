# VOS Ollama 模型预热与常驻

本文说明 HybRAG VOS app 在启动时如何确保 Ollama 模型已经下载、加载并常驻。

## 默认行为

HybRAG VOS app 会启动两个独立的 Ollama 容器：

- `hybrag-ollama-qa`：聊天和图片理解，默认模型 `qwen3.5:2b`。
- `hybrag-ollama-embedding`：向量化，默认模型 `bge-m3`。

两个容器都挂载 Model Hub 的共享 Ollama 目录：

```yaml
${MODEL_HUB_SHARED_MODELS_PATH:-/data/vos_workspace/model_hub}/ollama:/root/.ollama
```

因此 HybRAG 拉取或校验的模型文件会落在 Model Hub 约定的宿主机路径中，默认是：

```text
/data/vos_workspace/model_hub/ollama
```

VOS app profile 共有 6 个：`amd`、`amd-no-cuda`、`arm`、`arm-no-cuda`、`l4t`、`thor-spark`。其中 `amd-no-cuda` 和 `arm-no-cuda` 复用对应 CUDA profile 的镜像版本，但 Ollama 容器不配置 `runtime: nvidia`，用于没有 GPU runtime 的机器。

## 启动顺序

每个 HybRAG Ollama 容器启动时都会执行同一类流程：

1. 先启动本容器内的 `ollama serve`。
2. 等待本容器 `ollama list` 可用。
3. 通过 `MODEL_HUB_BACKEND_URL` 触发 Model Hub 拉取对应模型。
4. 如果 Model Hub 不可用、返回失败或超时，则退回本容器执行 `ollama pull`。
5. 执行 `ollama show` 做本地一致性确认；只有共享目录中仍不存在模型时，才执行本容器 `ollama pull`。
6. 通过本容器 Ollama API 发起 warmup 请求，并带上 `keep_alive=-1m`，让模型常驻。
7. 最后启动 `ollama_gateway`，对外提供 OpenAI-compatible `/v1` 接口。

HybRAG app 容器通过 `depends_on` 等待两个 Ollama 容器健康后才启动，所以 app 启动前会先完成模型准备。App 启动后的失败文档补交任务必须在后台执行，不能阻塞 `/health` 和 HTTP 监听；VOS 安装流程会在 app 健康前先启动 frontend，frontend 短暂 502 是可接受的，但不能因为 app 慢启动停在 `Created`。

VOS 包不会放额外 `config/` 目录；默认由 App 容器入口脚本在运行时生成 `builtin_models.yaml`。安装 UI 的 `HYBRAG_DEFAULT_BUILTIN_MODELS=true` 会自动创建三条默认模型行。界面里用 `display_name` 区分两个 Ollama 后端：`HybRAG Ollama QA (hybrag-ollama-qa)`、`HybRAG Ollama VLM (hybrag-ollama-qa)` 和 `HybRAG Ollama Embedding (hybrag-ollama-embedding)`。其中 QA/VLM 模型行的 `extra_config.thinking_control=think`，用于 Ollama Qwen3.5 关闭思考；vLLM / generic Qwen3.5 后端应使用 `chat_template_kwargs`，不要照搬 Ollama 的 `think`。如果需要覆盖默认模型行，在安装 UI 的 `HYBRAG_BUILTIN_MODELS_YAML` 填写完整 `builtin_models:` YAML。

## Model Hub alias

默认配置是：

```env
MODEL_HUB_BACKEND_URL=http://model-hub-backend:5005
```

这个地址必须能在 `vos_default` 网络内解析。Model Hub VOS app 的 compose 模板已经给后端服务配置了稳定 alias `model-hub-backend`。在 tc232 上已验证 HybRAG 容器内可以解析：

```bash
docker exec <hybrag-container> getent hosts model-hub-backend
```

如果其他机器上解析不到，优先检查 Model Hub 是否通过 VOS app 安装并运行在同一个 `vos_default` 网络中，而不是把 HybRAG 改成宿主机 IP。

## 可配置项

这些配置会在 HybRAG VOS 安装 UI 中暴露：

```env
MODEL_HUB_SHARED_MODELS_PATH=/data/vos_workspace/model_hub
MODEL_HUB_BACKEND_URL=http://model-hub-backend:5005
OLLAMA_QA_MODEL=qwen3.5:2b
OLLAMA_EMBEDDING_MODEL=bge-m3
OLLAMA_KEEP_ALIVE=-1m
OLLAMA_QA_NUM_PARALLEL=8
OLLAMA_EMBEDDING_NUM_PARALLEL=4
OLLAMA_CONTEXT_LENGTH=24000
OLLAMA_EMBEDDING_CONTEXT_LENGTH=8192
```

`OLLAMA_KEEP_ALIVE=-1m` 表示 warmup 后模型不卸载。普通 profile 默认 QA Ollama 总并发 8，embedding Ollama 总并发 4；应用侧默认给在线聊天预留 2 个 QA 槽位，文档 embedding 使用 2 个 embedding 槽位。

## 验证命令

在安装完成后，可以进入对应容器检查模型是否已经下载和常驻：

```bash
docker exec <hybrag-ollama-qa-container> ollama list
docker exec <hybrag-ollama-qa-container> ollama ps

docker exec <hybrag-ollama-embedding-container> ollama list
docker exec <hybrag-ollama-embedding-container> ollama ps
```

也可以从 HybRAG app 容器或同网络容器测试 gateway：

```bash
curl -fsS http://hybrag-ollama-qa:11535/v1/models
curl -fsS http://hybrag-ollama-embedding:11535/v1/models
```

测试 embedding：

```bash
curl -fsS http://hybrag-ollama-embedding:11535/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3","input":["中文知识库检索测试"]}'
```

## 常见问题

- `model-hub-backend` 解析失败：确认 Model Hub app 已安装、后端容器在 `vos_default` 网络中，且其 compose 模板保留 `model-hub-backend` alias。
- Model Hub 拉取失败但 HybRAG 仍能启动：HybRAG Ollama 容器会退回本容器 `ollama pull`，模型文件仍写入共享目录。
- 容器长时间不健康：看 Ollama 容器日志，通常是模型下载慢、网络不可达、磁盘不足或模型名写错。
- `ollama ps` 没有模型：确认 `OLLAMA_KEEP_ALIVE=-1m`，并检查 warmup 请求是否成功。
