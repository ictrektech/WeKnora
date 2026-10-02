# VOS Model Hub 模型预热与常驻

本文说明 HybRAG VOS app 如何复用 Model Hub 的预热 Ollama 运行时。HybRAG 现在不再启动自己的 Ollama 容器。

## 默认行为

Model Hub 应先安装并运行在同一个 `vos_default` 网络中。当前 HybRAG 默认引用三个 Model Hub 服务：

| 用途 | 服务名 | 原生 API | 可选 Gateway | 默认模型 |
| --- | --- | --- | --- | --- |
| QA / 聊天 / 图片理解 | `model-hub-ollama-qa` | `http://model-hub-ollama-qa:11434` | `http://model-hub-ollama-qa:11434` | `qwen3.5:2b` |
| Embedding | `model-hub-ollama-embedding` | `http://model-hub-ollama-embedding:11434` | `http://model-hub-ollama-embedding:11434` | `bge-m3` |
| ReRank | `model-hub-ollama-rerank` | `http://model-hub-ollama-rerank:11434` | `http://model-hub-ollama-rerank:11434` | `qllama/bge-reranker-v2-m3:q8_0` |

HybRAG 的默认模型直接调用 Model Hub Ollama 原生端口。Model Hub 管理模型存储和下载任务；HybRAG 打开时会检查默认模型，缺失时通过 Model Hub 管理 API 发起下载，并显示任务进度。默认模型名和地址由 HybRAG 包模板固定：

```env
OLLAMA_BASE_URL=http://model-hub-ollama-qa:11434
```

QA、VLM 和 embedding 使用 Ollama 原生的 OpenAI 兼容 `/v1` 接口；ReRank 使用原生 `/api/embed`。模型下载及进度走 Model Hub 的 `/api/v1/models/pull` 和 `/api/v1/tasks`，不依赖推理 Gateway。

模型行的 `base_url` 使用带 `/v1` 的 Ollama 地址；`OLLAMA_BASE_URL` 使用不带 `/v1` 的根地址。

## 启动顺序

1. 先安装并启动 Model Hub。
2. 在 Model Hub 运行管理页确认 `model-hub-ollama-qa`、`model-hub-ollama-embedding` 和 `model-hub-ollama-rerank` 在线。
3. 安装或启动 HybRAG；若默认模型缺失，页面会触发 Model Hub 下载并显示进度。

HybRAG app 启动后会用 `WEKNORA_REPARSE_WAIT_URLS` 等待两个 Ollama 的 `/v1/models` 可用，再执行失败文档补交。这个等待只影响后台补交，不阻塞 HTTP 服务启动。

## 默认模型行

VOS 包不会放额外 `config/` 目录；默认由 App 容器入口脚本在运行时生成 `builtin_models.yaml`，并自动创建四条默认模型行：

- `Model Hub Ollama QA (model-hub-ollama-qa)`：KnowledgeQA，endpoint `http://model-hub-ollama-qa:11434/v1`。
- `Model Hub Ollama VLM (model-hub-ollama-qa)`：VLLM，endpoint `http://model-hub-ollama-qa:11434/v1`。
- `Model Hub Ollama Embedding (model-hub-ollama-embedding)`：Embedding，endpoint `http://model-hub-ollama-embedding:11434/v1`。
- `Model Hub Ollama ReRank (model-hub-ollama-rerank)`：ReRank，endpoint `http://model-hub-ollama-rerank:11434`。

ReRank 只进入默认模型列表，不会自动写入知识库 `rerank_model_id`；是否在知识库、智能体或搜索流程里启用，由用户配置决定。

为了升级时不破坏已有引用，默认模型行的内部 `id` 会保持兼容；界面显示名和 endpoint 会跟随当前 YAML 托管配置同步。

## 思考参数兼容（当前源码已实现）

默认 QA、VLM 模型行已改用 `thinking_control=reasoning_effort`，匹配 Ollama 原生 `11434/v1`。本机 `qwen3.5:2b` / Ollama `0.32.14` 验证中，该接口忽略顶层 `think`，问题生成的 512-token 预算耗在思考上，正文为空。当前源码已修复；运行中的旧镜像仍需升级。

`extra_config.thinking_control` 指定 OpenAI 兼容接口的思考开关写法，须与实际后端匹配：

| 配置值 | 本次请求关闭思考 | 本次请求开启思考 | 适用后端 |
| --- | --- | --- | --- |
| `reasoning_effort` | 顶层 `"reasoning_effort": "none"` | 顶层 `"reasoning_effort": "medium"` | 当前默认 Ollama `11434/v1`；其他接口须确认支持这两个值 |
| `think` | 顶层 `"think": false` | 顶层 `"think": true` | 支持此字段的 Model Hub Gateway `11535/v1` |
| `chat_template_kwargs` | `"chat_template_kwargs": {"enable_thinking": false}` | `"chat_template_kwargs": {"enable_thinking": true}` | 使用此模板参数的 vLLM / generic 后端 |

`think` 和旧 `reasoning_effort` 配置只表达开关，模型能力列表仅提供 `off`、`auto`。未指定思考偏好时不发送开关，保留后端默认值；流式和非流式请求映射相同，不同时附加另一种开关或思考预算。VLM 的 OCR 调用未指定思考偏好，因此本次配置修正不会主动关闭 OCR 思考。其他供应商的适配策略保持原样。

问题生成显式关闭思考，保留 512-token 输出上限。增加上限不能替代正确的开关。空白或解析后零问题的响应现在返回错误，包含 `finish_reason` 和正文长度；手动重生成遇到该错误会保留原有问题。

整批生成失败会返回错误，沿用队列最多重试 3 次的策略。部分生成成功且成功结果已保存、索引成功时，日志记录 `partial_failure`、处理追踪记录 `QUESTION_FAILED`，不自动重跑整批；索引失败仍返回错误，触发整批重试。旧的整文档任务采用相同规则。最终失败仍释放子任务计数，文档可以完成解析；解析完成不代表每个片段都有问题。部分失败的片段需手动补生成。

升级后的启动同步会更新 `managed_by=yaml` 的默认模型行；已由管理员接管的模型行需自行检查参数。升级不会自动补齐历史空结果。先验证一个片段能生成问题及会话推荐接口，再补生成已有文档的问题。

回归测试覆盖默认配置与实际请求格式、空输出、整批失败重试、部分成功保留结果、重试耗尽释放计数，以及旧任务兼容。运行镜像升级和历史补生成仍需单独验证。

## 验证命令

在同一 Docker 网络中测试：

```bash
curl -fsS http://model-hub-ollama-qa:11434/v1/models
curl -fsS http://model-hub-ollama-embedding:11434/v1/models

curl -fsS http://model-hub-ollama-embedding:11434/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3","input":["中文知识库检索测试"]}'

curl -fsS http://model-hub-ollama-rerank:11434/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"qllama/bge-reranker-v2-m3:q8_0","input":["Query: 中文知识库检索测试\nDocument: 中文知识库检索测试","Query: 中文知识库检索测试\nDocument: 无关内容"]}'
```

如果在宿主机上测试，需要使用 Model Hub 对外映射的端口或进入任意 `vos_default` 网络内的容器执行。

## 常见问题

- `model-hub-ollama-qa` 或 `model-hub-ollama-embedding` 解析失败：确认 Model Hub 已安装、容器在 `vos_default` 网络中，并保留这两个服务 alias。
- HybRAG 模型列表为空：先检查 Model Hub 两个 Ollama 的 `/v1/models`，再检查 App 容器启动日志中默认 `builtin_models.yaml` 是否生成。
- 聊天一直“正在思考”：先在 Model Hub QA 容器内确认模型是否常驻并有可用槽位，再检查原生 `11434/v1` 模型行是否使用 `thinking_control=reasoning_effort`。
- 文档解析 embedding 失败：测试 `model-hub-ollama-embedding:11434/v1/embeddings`，确认模型名与 HybRAG 模型行一致。
- ReRank 不可用：测试 `model-hub-ollama-rerank:11434/api/embed`，确认 `qllama/bge-reranker-v2-m3:q8_0` 已在 Model Hub rerank worker 中下载、常驻并保留可用槽位。
