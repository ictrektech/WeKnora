# VOS 外部调用 HybRAG API 实测流程

[返回目录](./README.md)

本文说明外部系统不在 VOS iframe 内时，如何以普通 VOS 用户身份访问 HybRAG API。示例基于 VOS App 默认端口：

- HybRAG 前端：`http://<VOS主机IP>:29080`
- HybRAG API：`http://<VOS主机IP>:29081/api/v1`
- VOS API：`http://<VOS主机IP>:8105`

如果安装 HybRAG 时修改过 `HYBRAG_FRONTEND_PORT` 或 `HYBRAG_API_PORT`，请使用实际安装端口。

229 环境的完整实测结果和客户需求覆盖表见：[VOS 外部 API 验证与客户需求覆盖](./vos-external-api-validation.md)。

## 权限模型

外部应用访问用户自己的知识库不需要 admin 账号，有两条方式：

1. 普通用户个人 API Key：普通用户登录 HybRAG 后，在「设置 -> API 集成」创建自己的 API Key。该 Key 只绑定当前用户，只能访问创建时选择的知识库范围。
2. VOS/OAuth Bearer Token：外部应用先通过 VOS 登录或 OAuth 流程拿到普通用户的 VOS access token，再作为 `Authorization: Bearer <VOS_ACCESS_TOKEN>` 调用 HybRAG。HybRAG 会校验该 token，映射为对应本地用户，并按该用户权限执行。

只有平台级管理员（`CanAccessAllTenants=true`）可以创建工作区级 API Key。普通用户即使是自己个人空间的 Owner，也只能创建个人 API Key，不能创建 full-access 或成员、模型、数据源等高权限 Key。

## 方式一：普通用户个人 API Key

适合服务端长期集成。用户先在 HybRAG UI 中创建 API Key，并绑定允许访问的知识库。

后续请求使用：

```bash
BASE="http://<VOS主机IP>:29081/api/v1"
API_KEY="sk_xxx"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY"
```

创建会话：

```bash
SESSION_ID=$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部 API 测试"}' | jq -r '.data.id')
```

指定知识库发起 RAG 问答：

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "这份知识库里有哪些重点？",
    "knowledge_base_ids": ["<knowledge_base_id>"],
    "channel": "api"
  }'
```

无会话检索：

```bash
curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "检索关键词",
    "knowledge_base_ids": ["<knowledge_base_id>"]
  }'
```

## 方式二：VOS 用户 Token

适合需要严格继承 VOS 当前用户身份和权限的场景。

### 1. 获取 VOS access token

VOS OAuth token endpoint 不支持 password grant；浏览器或 VOS App 内推荐走 authorization code / Fastpath。命令行验证时，可以调用 VOS 登录接口取得普通用户 access token：

```bash
VOS_BASE="http://<VOS主机IP>:8105"
VOS_USER="<普通用户名>"
VOS_PASS="<普通用户密码>"

VOS_TOKEN=$(curl -sS -X POST "$VOS_BASE/v1000/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$VOS_USER\",\"password\":\"$VOS_PASS\"}" \
  | jq -r '.data.access_token')
```

生产集成如果已经在 VOS OAuth 流程中拿到了 access token，可直接使用该 token，不需要再调用用户名密码登录。

### 2. 直接调用 HybRAG

```bash
BASE="http://<VOS主机IP>:29081/api/v1"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

也可以先交换成 HybRAG 本地登录态，再用返回的 HybRAG token 调用后续接口：

```bash
HYBRAG_TOKEN=$(curl -sS -X POST "$BASE/auth/vos-oidc" \
  -H "Content-Type: application/json" \
  -d "{\"access_token\":\"$VOS_TOKEN\"}" \
  | jq -r '.token')
```

### 3. 创建个人 API Key

普通用户的个人 API Key 必须绑定知识库范围。先列出当前用户可见的知识库：

```bash
curl -sS "$BASE/knowledge-bases?creator=all" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

创建只允许检索和问答的个人 API Key：

```bash
KB_ID="<knowledge_base_id>"

API_KEY=$(curl -sS -X POST "$BASE/tenants/<tenant_id>/api-keys" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"external-rag-readonly\",
    \"full_access\": false,
    \"capabilities\": [\"retrieve\", \"chat\"],
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq -r '.data.api_key')
```

此后外部服务即可使用方式一中的 `X-API-Key` 调用 RAG API。

## 指定知识库和会话

- `knowledge_base_ids` 是推荐字段，可传一个或多个知识库 ID。
- `knowledge_base_id` 是旧版单知识库字段，仅为兼容保留。
- `knowledge_ids` 用于指定单个或多个文档。HybRAG API 里的 `knowledge_id` 对应业务语义中的文档 ID；上传文件、导入 URL 或创建手动知识后，响应里的 `data.id` 就是该文档的 `knowledge_id`。
- 如果只想在某一份或几份文档内检索，把这些文档 ID 放到 `knowledge_ids`；字段名不是 `document_id` 或 `document_ids`。
- `session_id` 由 `POST /sessions` 返回；同一用户多轮对话复用同一个 `session_id`，新话题可以重新创建。
- 个人 API Key 请求中的 `knowledge_base_ids` 必须在 Key 创建时绑定的范围内，否则会返回 `403`。

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"只根据指定文档回答这个问题\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## 229 实测结论

在 `192.168.1.229` 上实测确认：

- VOS 普通用户可以通过 `/v1000/user/login` 取得 VOS access token。
- HybRAG `/auth/vos-oidc` 可以校验该 token，并映射为 HybRAG 普通用户。
- 普通用户 `can_access_all_tenants=false`，不应创建工作区级高权限 API Key。
- 修复后，普通用户只能创建绑定知识库范围的个人 API Key；`manage_members`、`manage_models`、`manage_datasources`、`full_access` 等工作区级权限只对平台级管理员开放。
