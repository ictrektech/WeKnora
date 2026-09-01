# WeKnora API 文档

## 目录

- [概述](#概述)
- [最权威参考：Swagger UI](#最权威参考swagger-ui)
- [基础信息](#基础信息)
- [认证机制](#认证机制)
- [非 VOS 场景调用 RAG API](#非-vos-场景调用-rag-api)
- [VOS 外部调用实测流程](./vos-external-api.md)
- [错误处理](#错误处理)
- [文件与图片引用（`resource://` 与直链）](#文件与图片引用resource-与直链)
- [API 概览](#api-概览)

## 概述

WeKnora 提供了一系列 RESTful API，用于创建和管理知识库、检索知识，以及进行基于知识的问答。本文档详细描述了这些 API 的使用方式。

## 最权威参考：Swagger UI

WeKnora 同时提供基于 OpenAPI 的 Swagger 文档。**启动服务后访问 `http://localhost:8080/swagger/index.html`**，可看到所有端点的完整参数、请求/响应 schema，并可直接在浏览器内试调——它随代码自动更新，是最准确的接口参考。

本目录下的 markdown 文档提供更易读的示例与场景说明，与 swagger 同步维护；当二者出现差异时，以 swagger 为准。

> Swagger UI 仅在非 release 模式（`GIN_MODE != release`）下挂载；生产部署默认关闭。

## 基础信息

- **基础 URL**: `/api/v1`
- **响应格式**: JSON
- **认证方式**: `X-API-Key` 或 `Authorization: Bearer <token>`

VOS app 安装时，HybRAG 后端在容器内监听 `8080`，同时提供两种访问方式：

- VOS 应用路径：`https://<VOS地址>/app/com.ictrek.hybrag/api/v1`
- 宿主机直连端口：`http://<主机IP>:29081/api/v1`

VOS 包默认还把前端映射到 `http://<主机IP>:29080`。如果安装时修改过 `HYBRAG_API_PORT` 或 `HYBRAG_FRONTEND_PORT`，以上端口以安装配置为准。旧独立 compose 或调试部署可能使用其他端口，例如历史模板中的 `19081`。

## 认证机制

业务 API 请求需要在 HTTP 请求头中携带一种有效凭证。

推荐服务端集成优先使用普通用户自己的 API Key：

```
X-API-Key: your_api_key
```

已登录 HybRAG 用户、OIDC/VOS 自动登录后的调用，也可以使用 Bearer Token。VOS/OAuth access token
会由 HybRAG 后端向 VOS userinfo 接口校验，校验成功后按对应 VOS 用户映射到 HybRAG 用户并执行权限判断：

```
Authorization: Bearer <access_token>
```

为便于问题追踪和调试，建议每个请求的 HTTP 请求头中添加 `X-Request-ID`：

```
X-Request-ID: unique_request_id
```

### 获取 API Key

在「设置 → API 集成」创建 API Key。

- 普通用户创建的是**个人 API Key**，只绑定当前用户，可选择 `retrieve`、`chat`、`ingest` 等能力，并且必须限制到自己可访问的知识库范围；即使该用户是自己个人空间的 Owner，也不会因此获得工作区级 API Key 权限。
- 平台级管理员（`CanAccessAllTenants=true`）可以创建工作区级 API Key，可按需开放 `manage_kbs`、`manage_models` 等空间管理能力，或创建 full-access Key。

请妥善保管 API Key，避免泄露。明文 Key 只在创建时返回；如需更换，删除旧 Key 后重新创建。

## 非 VOS 场景调用 RAG API

HybRAG 作为 VOS App 打开时，前端会尝试使用 VOS 当前用户自动登录。不在 VOS iframe 内运行的外部服务、脚本或浏览器插件可以通过 VOS 应用路径或宿主机直连端口调用 HybRAG REST API。当前支持两条认证路径：

1. **个人 API Key**：普通用户在 HybRAG「API 集成」里为自己创建 API Key，外部应用发送 `X-API-Key`。这是最简单、长期稳定的服务端集成方式；Key 只继承该用户自己的知识库访问范围，不需要 admin 账号预先代发。
2. **VOS/OAuth Bearer token**：外部应用先通过 VOS/OAuth 登录流程取得该普通用户的 VOS access token，再发送 `Authorization: Bearer <VOS token>`。HybRAG 会调用 VOS `/v1000/oauth2/userinfo` 校验；失败时按旧 VOS `/v1000/user/check` 降级。校验成功后按该 VOS 用户映射出的 HybRAG 用户权限执行。

因此，外部应用访问用户自己的知识库**不需要 admin 账号**。使用个人 API Key 时由该普通用户自己创建；使用 VOS/OAuth Bearer token 时继承该 VOS 用户映射后的 HybRAG 权限。

### RAG Query / Chat API

典型流程是先创建会话，再发起流式 RAG 问答：

```bash
BASE=https://example.com/api/v1
API_KEY=sk_xxx

SESSION_ID=$(curl -s -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{}' | jq -r '.data.id')

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"query":"这份知识库里有哪些重点?","knowledge_base_ids":["<knowledge_base_id>"],"channel":"api"}'
```

如果调用方已经拿到 VOS/OAuth access token，也可以直接把上例中的 `X-API-Key` 换成：

```bash
VOS_TOKEN=eyJ...

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"query":"这份知识库里有哪些重点?","knowledge_base_ids":["<knowledge_base_id>"],"channel":"api"}'
```

- `POST /knowledge-chat/:session_id` 是知识库 RAG 问答，返回 SSE 流。
- `POST /agent-chat/:session_id` 是智能体问答，支持工具、MCP、联网搜索等 Agent 能力。
- `POST /knowledge-search` 是无会话知识检索，返回一次性 JSON，不调用 LLM 生成答案。

### 指定 Knowledge Base / Knowledge Base ID

- RAG Chat 使用请求体字段 `knowledge_base_ids` 指定一个或多个知识库 ID，例如 `["kb-1"]`。
- 无会话检索 `POST /knowledge-search` 也支持 `knowledge_base_ids`；`knowledge_base_id` 仅作为旧版单库字段保留。
- 如果 API Key 创建时设置了知识库范围，请求中的知识库 ID 必须在该范围内。

### Conversation / Session ID

- `session_id` 来自 `POST /sessions` 响应中的 `data.id`。
- 同一终端用户继续多轮对话时复用同一个 `session_id`；新话题可以重新创建会话。
- 使用个人 API Key 或 VOS/OAuth Bearer token 时，调用主体已经是具体用户，不需要再配置 `X-External-User-ID`。
- 使用工作区级 API Key 接入并需要区分终端用户时，在「API 集成」配置用户身份模式：可信服务端可用 `X-External-User-ID`，面向用户的应用建议使用 `X-External-User-Token` 签名 Token。

### Authentication / Service Account

- 外部服务不需要每次用 admin 账号登录获取 Token。
- 普通用户可以创建自己的个人 API Key，第三方服务用 `X-API-Key` 长期调用，权限边界由 Key 的能力和知识库范围决定。
- 需要严格继承 VOS 当前用户权限时，外部应用可以使用普通用户的 VOS/OAuth access token 作为 `Authorization: Bearer <token>`；HybRAG 校验后按映射用户权限执行。
- API Key 不放进 Bearer 头；API Key 使用 `X-API-Key`，VOS/OAuth 或 HybRAG 用户 token 使用 `Authorization: Bearer`。

## 错误处理

所有 API 使用标准的 HTTP 状态码表示请求状态，并返回统一的错误响应格式：

```json
{
  "success": false,
  "error": {
    "code": "错误代码",
    "message": "错误信息",
    "details": "错误详情"
  }
}
```

## 文件与图片引用（`resource://` 与直链）

响应里的图片、图表、附件默认以内部引用 `resource://<handle>` 返回，例如问答答案中的
`![示意图](resource://xifDo7NTSL300Lp1goVutw)`。这类引用不能被浏览器直接加载，客户端需要再
调用带鉴权的 `GET /files?file_path=<引用>` 代理去取字节流。

如果你在把 WeKnora 集成进自己的 App，可以让服务端直接返回**可加载的 http(s) 直链**，省掉这一次
额外请求：

| 方式 | 用法 | 生效范围 |
|------|------|----------|
| 单次请求 | 在 URL 上加 `?resource_urls=public` | 仅该次请求 |
| 整个部署 | 环境变量 `RESOURCE_URL_MODE=public` | 所有未显式传参的请求 |

`resource_urls` 取值为 `handle`（默认，保持内部引用）或 `public`（返回直链）；传其它值返回
`400`。单次请求的参数优先于环境变量，因此把部署默认设成 `public` 后，仍可用
`?resource_urls=handle` 单独退回。

支持该参数的接口：

- `POST /api/v1/knowledge-chat/{session_id}`（SSE）
- `POST /api/v1/agent-chat/{session_id}`（SSE）
- `GET /api/v1/sessions/continue-stream/{session_id}`（SSE）
- `GET /api/v1/messages/{session_id}/load`
- `POST /api/v1/knowledge-search`

改写覆盖答案正文、`knowledge_references`（含 `image_info`）、Agent 执行步骤与工具结果，以及消息
上的图片附件。流式回答里跨两个 chunk 被截断的引用会先缓冲再改写，客户端拿到的始终是完整链接。

### 注意事项

- **需要外链能力。** 直链由存储后端预签名，或由 `APP_EXTERNAL_URL` + `/r/<token>` 提供。二者都不
  可用时（例如 local 存储且未设 `APP_EXTERNAL_URL`），该引用**保持 `resource://` 原样**，客户端
  仍可回退到 `/files` 代理。详见 `.env.example` 中的 `APP_EXTERNAL_URL` 说明。
- **直链是限时匿名可读的**（WeKnora 签发的 grant 2 小时，MinIO 预签名 24 小时）。任何拿到链接的
  人在过期前都能读取该文件，请勿写入日志或转发给不应看到该文件的一方。
- **嵌入式（embed）渠道不支持该参数。** 其访客是匿名的，`/api/v1/embed/...` 下的接口会强制使用
  `handle`（即使传了 `?resource_urls=public`、或部署默认是 `public`），图片仍走渠道维度的鉴权代理。
- **限定知识库的 API Key 不能使用 `public`**，返回 `403`。这类 Key 本身也被拒绝访问 `/files`
  代理，若能拿到匿名直链等于绕过同一道限制。改用 `handle` 即可正常调用。
- **同一文件的直链会在有效期内复用**：重复请求不会反复签发凭证，也不会每次都拿到不同的 URL，客户端
  和 CDN 的缓存因此可以命中。凭证被吊销或过期后链接立即失效。

## API 概览

WeKnora API 按功能分为以下几类：

| 分类 | 描述 | 文档链接 |
|------|------|----------|
| 认证管理 | 用户注册、登录、令牌管理；OIDC 流程 | [auth.md](./auth.md) · [OIDC认证调用流程.md](../OIDC认证调用流程.md) |
| 空间管理 | 创建和管理空间账户 | [tenant.md](./tenant.md) |
| 知识库管理 | 创建、查询和管理知识库 | [knowledge-base.md](./knowledge-base.md) |
| 知识管理 | 上传、检索和管理知识内容 | [knowledge.md](./knowledge.md) |
| 模型管理 | 配置和管理各种AI模型 | [model.md](./model.md) |
| 分块管理 | 管理知识的分块内容 | [chunk.md](./chunk.md) |
| 标签管理 | 管理知识库的标签分类 | [tag.md](./tag.md) |
| FAQ管理 | 管理FAQ问答对 | [faq.md](./faq.md) |
| 智能体管理 | 创建和管理自定义智能体 | [agent.md](./agent.md) |
| 会话管理 | 创建和管理对话会话 | [session.md](./session.md) |
| 知识搜索 | 在知识库中搜索内容 | [knowledge-search.md](./knowledge-search.md) |
| 聊天功能 | 基于知识库和 Agent 进行问答 | [chat.md](./chat.md) |
| 消息管理 | 获取和管理对话消息 | [message.md](./message.md) |
| 评估功能 | 评估模型性能 | [evaluation.md](./evaluation.md) |
| 初始化管理 | 知识库模型配置与 Ollama 管理 | [initialization.md](./initialization.md) |
| 系统管理 | 系统信息、解析引擎、存储引擎 | [system.md](./system.md) |
| MCP 服务 | MCP 工具服务管理 | [mcp-service.md](./mcp-service.md) |
| 组织管理 | 组织、成员、知识库/智能体共享 | [organization.md](./organization.md) |
| Skills | 预装智能体技能 | [skill.md](./skill.md) |
| 网络搜索 | 网络搜索服务商 | [web-search.md](./web-search.md) |
| 向量存储 | 向量数据库连接管理 | [vector-store.md](./vector-store.md) |
| 存储后端 | 对象/文件存储实例（多实例）管理 | [storage-backend.md](./storage-backend.md) |
| IM 渠道 | 企业微信 / 飞书 / Slack 等 IM 平台对接，含渠道 CRUD 与回调 | [../IM集成开发文档.md](../IM集成开发文档.md) |
| 数据源导入 | 飞书 / 企微 / Notion / Confluence 等外部数据源接入与同步 | [../数据源导入开发文档.md](../数据源导入开发文档.md) |
