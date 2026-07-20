# HybRAG

本文档与 `README.zh-CN.md` 保持同一套部署说明。当前 VOS 包默认复用 Model Hub 已预热并常驻的两个 Ollama 运行时，不再启动 HybRAG 自己的 Ollama 容器。

## 组件

- HybRAG Web 前端
- HybRAG App API
- DocReader 文档解析服务
- Agent Skills sandbox 镜像
- Redis
- Neo4j 知识图谱数据库
- 外部 PGV/Postgres 依赖
- 外部 Model Hub 依赖

## 依赖

`manifest.yml` 要求：

- `com.ictrek.model-hub >= 0.0.27`：提供 `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 两个预热运行时。
- `com.ictrek.pgv >= 0.0.13`：提供 `shared-pgv:5432` Postgres/pgvector 服务。

HybRAG 的 `docker-compose.yml` 不启动 Model Hub 或 Postgres。

## 默认模型

默认安装时 `HYBRAG_DEFAULT_BUILTIN_MODELS=true`，App 容器入口脚本会生成三条 YAML 托管模型行：

| 类型 | display_name | endpoint | 默认模型 |
| --- | --- | --- | --- |
| KnowledgeQA | `Model Hub Ollama QA (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` | `qwen3.5:2b` |
| VLLM | `Model Hub Ollama VLM (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` | `qwen3.5:2b` |
| Embedding | `Model Hub Ollama Embedding (model-hub-ollama-embedding)` | `http://model-hub-ollama-embedding:11535/v1` | `bge-m3` |

安装 UI 可调整：

```env
OLLAMA_QA_MODEL=qwen3.5:2b
OLLAMA_EMBEDDING_MODEL=bge-m3
MODEL_HUB_OLLAMA_QA_API_URL=http://model-hub-ollama-qa:11434
MODEL_HUB_OLLAMA_QA_GATEWAY_URL=http://model-hub-ollama-qa:11535/v1
MODEL_HUB_OLLAMA_EMBEDDING_GATEWAY_URL=http://model-hub-ollama-embedding:11535/v1
```

模型行必须使用 `11535/v1` Gateway 地址，不能改成 Ollama 原生 `11434`。只有经过 Gateway 的请求才会被 Model Hub 统计槽位、运行阶段和 token/s。

Model Hub 负责模型下载、预热、常驻、上下文和 Ollama 并发；HybRAG 只负责引用 gateway 并做应用侧并发调度。

## 排错

详细预热、常驻和排错说明见 `docs/ictrek/vos-ollama-prewarm.md`。
