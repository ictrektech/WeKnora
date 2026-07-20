# VOS Model Hub 模型预热与常驻

本文说明 HybRAG VOS app 如何复用 Model Hub 的两个预热 Ollama 运行时。HybRAG 现在不再启动自己的 Ollama 容器。

## 默认行为

Model Hub 应先安装并运行在同一个 `vos_default` 网络中。当前 HybRAG 默认引用两个 Model Hub 服务：

| 用途 | 服务名 | API | Gateway | 默认模型 |
| --- | --- | --- | --- | --- |
| QA / 聊天 / 图片理解 | `model-hub-ollama-qa` | `http://model-hub-ollama-qa:11434` | `http://model-hub-ollama-qa:11535` | `qwen3.5:2b` |
| Embedding | `model-hub-ollama-embedding` | `http://model-hub-ollama-embedding:11434` | `http://model-hub-ollama-embedding:11535` | `bge-m3` |

Model Hub 负责模型下载、预热、常驻、上下文长度和 Ollama 并发。HybRAG 只在默认模型行里引用 OpenAI-compatible gateway：

```env
MODEL_HUB_OLLAMA_QA_GATEWAY_URL=http://model-hub-ollama-qa:11535/v1
MODEL_HUB_OLLAMA_EMBEDDING_GATEWAY_URL=http://model-hub-ollama-embedding:11535/v1
OLLAMA_BASE_URL=http://model-hub-ollama-qa:11434
OLLAMA_QA_MODEL=qwen3.5:2b
OLLAMA_EMBEDDING_MODEL=bge-m3
```

QA、VLM 和 embedding 模型行必须配置到 `11535/v1` Gateway。不要配置到 Ollama 原生 `11434`，否则 Model Hub 只能看到服务在线，看不到 WeKnora 请求的槽位、阶段和 token/s。

`OLLAMA_BASE_URL` 只用于兼容本地 Ollama 类配置和服务状态检查；默认聊天、VLM、embedding 三条模型行都使用 gateway 地址。

## 启动顺序

1. 先安装并启动 Model Hub。
2. 在 Model Hub 运行管理页确认 `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 在线。
3. 确认 `qwen3.5:2b` 和 `bge-m3` 已下载并处于运行中。
4. 再安装或启动 HybRAG。

HybRAG app 启动后会用 `WEKNORA_REPARSE_WAIT_URLS` 等待两个 Model Hub gateway 的 `/v1/models` 可用，再执行失败文档补交。这个等待只影响后台补交，不应该阻塞 HybRAG HTTP 服务启动。

## 默认模型行

VOS 包不会放额外 `config/` 目录；默认由 App 容器入口脚本在运行时生成 `builtin_models.yaml`。安装 UI 的 `HYBRAG_DEFAULT_BUILTIN_MODELS=true` 会自动创建三条默认模型行：

- `Model Hub Ollama QA (model-hub-ollama-qa)`：KnowledgeQA，endpoint `http://model-hub-ollama-qa:11535/v1`。
- `Model Hub Ollama VLM (model-hub-ollama-qa)`：VLLM，endpoint `http://model-hub-ollama-qa:11535/v1`。
- `Model Hub Ollama Embedding (model-hub-ollama-embedding)`：Embedding，endpoint `http://model-hub-ollama-embedding:11535/v1`。

为了升级时不破坏已有引用，默认模型行的内部 `id` 会保持兼容；界面显示名和 endpoint 会跟随当前 YAML 托管配置同步。

Ollama Qwen3.5 关闭思考使用 `extra_config.thinking_control=think`，请求会发送顶层 `think:false`。vLLM / generic Qwen3.5 后端应使用 `chat_template_kwargs`，不要照搬 Ollama 的 `think`。

## 验证命令

在同一 Docker 网络中测试：

```bash
curl -fsS http://model-hub-ollama-qa:11535/v1/models
curl -fsS http://model-hub-ollama-embedding:11535/v1/models

curl -fsS http://model-hub-ollama-embedding:11535/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3","input":["中文知识库检索测试"]}'
```

如果在宿主机上测试，需要使用 Model Hub 对外映射的端口或进入任意 `vos_default` 网络内的容器执行。

## 常见问题

- `model-hub-ollama-qa` 或 `model-hub-ollama-embedding` 解析失败：确认 Model Hub 已安装、容器在 `vos_default` 网络中，并保留这两个服务 alias。
- HybRAG 模型列表为空：先检查 Model Hub 两个 gateway 的 `/v1/models`，再检查 HybRAG 默认模型 YAML 是否被 `HYBRAG_BUILTIN_MODELS_YAML` 覆盖。
- 聊天一直“正在思考”：先在 Model Hub QA 容器内确认模型是否常驻并有可用槽位，再检查 HybRAG 模型行是否使用 `thinking_control=think`。
- 文档解析 embedding 失败：测试 `model-hub-ollama-embedding:11535/v1/embeddings`，确认模型名与 HybRAG 模型行一致。
