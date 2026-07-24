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

- `com.ictrek.model-hub >= 0.0.54`：最低兼容版本；提供 `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 两个预热运行时，当前 HybRAG 需要使用同时兼容 OpenAI `/v1/*` 与 Ollama `/api/*` 的 `11535` gateway。
- `com.ictrek.pgv >= 0.0.21`：最低兼容版本；提供 `shared-pgv:5432` Postgres/pgvector 服务。

HybRAG 的 `docker-compose.yml` 不启动 Model Hub 或 Postgres。

## VOS User Identity For Other Apps

Other VOS apps should not share or hard-code a HybRAG API Key when they need to access HybRAG as the currently opened VOS user. Use HybRAG's VOS token exchange endpoint instead:

```http
POST /api/v1/auth/vos-token-exchange
Authorization: Bearer <VOS access token>
```

JSON body is also supported:

```json
{
  "access_token": "<VOS access token>"
}
```

Recommended flow:

1. The user opens another app inside VOS.
2. That app obtains the current user's VOS access token from the VOS session or official VOS SDK.
3. That app forwards the VOS token to HybRAG `/api/v1/auth/vos-token-exchange`.
4. HybRAG verifies the token through VOS `/v1000/user/check`.
5. HybRAG maps the VOS username to `${username}@local` and creates the user plus personal workspace on first access.
6. The caller then uses the returned HybRAG `token` for HybRAG API requests.

Identity mapping is stable across VOS apps:

| VOS user | HybRAG account | Default workspace |
| --- | --- | --- |
| `admin` | `admin@local` | `admin's Workspace` |
| `alice` | `alice@local` | `alice's Workspace` |

Before VOS provides stable OIDC or official iframe user injection, frontend-only apps may follow HybRAG's temporary probing order: `window.__VOS_APP_CONTEXT__.accessToken`, `window.__VOS_APP_CONTEXT__.token`, `window.__VOS_ACCESS_TOKEN__`, then same-origin `localStorage` stores ending with `-core-access`. The localStorage/secure-ls path is a transition layer only. New apps should switch to the official VOS identity mechanism once it is available.

`X-External-User-ID` is not a keyless login mechanism. It only works together with a valid HybRAG API Key for trusted server-side integrations that need end-user session isolation.

## 默认模型

The app container entrypoint generates three YAML-managed model rows at runtime:

| 类型 | display_name | endpoint | 默认模型 |
| --- | --- | --- | --- |
| KnowledgeQA | `Model Hub Ollama QA (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` | `qwen3.5:2b` |
| VLLM | `Model Hub Ollama VLM (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` | `qwen3.5:2b` |
| Embedding | `Model Hub Ollama Embedding (model-hub-ollama-embedding)` | `http://model-hub-ollama-embedding:11535/v1` | `bge-m3` |

The default model names are fixed to `qwen3.5:2b` and `bge-m3` in the HybRAG package. Model download, prewarm, context size, and Ollama concurrency are configured in Model Hub, not in the HybRAG install form.

模型行必须使用 `11535/v1` Gateway 地址，不能改成 Ollama 原生 `11434`。只有经过 Gateway 的请求才会被 Model Hub 统计槽位、运行阶段和 token/s。

Model Hub 负责模型下载、预热、常驻、上下文和 Ollama 并发；HybRAG 只负责引用 gateway 并做应用侧并发调度。

## 排错

详细预热、常驻和排错说明见 `ictrek.app/docs/vos-ollama-prewarm.md`。
