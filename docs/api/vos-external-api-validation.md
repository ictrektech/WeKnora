# VOS 外部 API 接入检查清单

[返回目录](./README.md)

本文提供外部系统接入 HybRAG API 时的配置检查、调用顺序和权限边界说明。内容适用于 HybRAG 作为 VOS App 安装后的场景，也适用于通过宿主机映射端口直接访问 RAG API 的集成方式。

## 接入地址

HybRAG API 统一使用 `/api/v1` 前缀。VOS App 模板默认提供两类访问方式：

- VOS 应用路径：`/app/com.ictrek.hybrag/api/v1`
- 宿主机直连：`http://<VOS主机IP>:29081/api/v1`

前端页面默认映射到 `http://<VOS主机IP>:29080`。如果安装时调整过 `HYBRAG_FRONTEND_PORT` 或 `HYBRAG_API_PORT`，请以实际端口为准。

## 认证路径

HybRAG 支持两条外部认证路径。

### 普通用户个人 API Key

普通用户登录 HybRAG 后，在「设置 -> API 集成」创建个人 API Key，并绑定允许访问的知识库范围。外部服务端保存该 Key 后，用 `X-API-Key` 调用 API。

个人 API Key 只代表创建它的用户：

- 不需要 admin 代发。
- 不具备空间级 full access。
- 可用能力限制为知识检索、知识问答和文档写入等个人范围能力。
- 访问范围受创建时选择的 `knowledge_base_ids` 限制。

### VOS/OAuth Bearer Token

外部应用也可以先通过 VOS 登录或 OAuth 授权流程取得普通用户的 access token，再以 `Authorization: Bearer <VOS_ACCESS_TOKEN>` 调用 HybRAG。

HybRAG 会校验该 VOS token，映射为对应 HybRAG 用户，并按该用户权限执行。该方式适合需要严格继承 VOS 当前用户身份和权限的集成。

## 调用顺序

### 1. 确认调用主体

```bash
BASE="http://<VOS主机IP>:29081/api/v1"
API_KEY="<普通用户个人 API Key>"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY"
```

使用 VOS token 时：

```bash
VOS_TOKEN="<普通用户 VOS/OAuth access token>"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN"
```

### 2. 列出可访问知识库

```bash
curl -sS "$BASE/knowledge-bases?creator=all&page=1&page_size=20" \
  -H "X-API-Key: $API_KEY"
```

返回的知识库 `id` 用作后续请求里的 `knowledge_base_ids`。

### 3. 创建会话

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部系统 RAG 会话"}' \
  | jq -r '.data.id')"
```

`session_id` 用于多轮对话。新话题可以重新创建会话；同一话题可以复用同一个会话。

### 4. 无会话检索

```bash
KB_ID="<knowledge_base_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"检索关键词\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }"
```

### 5. RAG 问答

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"请根据知识库回答问题\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## 限定到指定文档

业务系统里的 `document_id` 对应 HybRAG API 中的 `knowledge_id`。

可以通过以下方式取得 `knowledge_id`：

- 上传文件、导入 URL 或创建手动知识后，响应里的 `data.id`。
- 知识库文档列表接口返回的每条记录 `id`。

在请求中使用 `knowledge_ids` 限定到一份或几份文档：

```bash
KNOWLEDGE_ID="<knowledge_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"只检索这份文档里的内容\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"]
  }"
```

对于绑定知识库范围的个人 API Key，带 `knowledge_ids` 时建议同时传入对应的 `knowledge_base_ids`，用于明确验证该文档限定发生在授权知识库范围内。

## 能力边界

| 能力 | HybRAG API 支持情况 | 说明 |
| --- | --- | --- |
| 普通用户访问自己的 RAG | 支持 | 通过个人 API Key 或 VOS/OAuth token |
| 指定 Knowledge Base | 支持 | 使用 `knowledge_base_ids` |
| 指定单个或多个文档 | 支持 | 使用 `knowledge_ids` |
| 会话复用 | 支持 | `POST /sessions` 返回 `session_id` |
| RAG Chat | 支持 | `POST /knowledge-chat/:session_id`，SSE 输出 |
| Retrieval | 支持 | `POST /knowledge-search` |
| Citation / Sources | 支持 | 检索结果和问答事件包含来源信息，具体内容取决于是否命中文档 |
| API Key 创建、列表、更新、删除 | 支持 | 普通用户创建个人 Key，管理员可创建工作区级 Key |
| API Key 过期时间 | 支持 | 创建或更新时可设置 `expires_at_unix` |
| API Key 轮换 | 通过重建实现 | 创建新 Key、切换调用方、删除旧 Key |
| NAS ACL、用户组、全局审计 | 平台能力 | HybRAG 继承 VOS 身份，但不替代 VOS/NAS 平台 API |

## 集成建议

- 服务端长期集成优先使用个人 API Key，便于绑定能力和知识库范围。
- 需要严格继承当前 VOS 用户权限时，使用 VOS/OAuth token。
- 终端设备不应保存 admin 凭证、平台级 API Key 或用户密码。
- NAS 文件权限和 RAG 知识库权限应分开设计：文件可读不代表自动进入知识库，发布到 RAG 前应有明确流程。
