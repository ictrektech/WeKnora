# VOS 外部 API 验证与客户需求覆盖

[返回目录](./README.md)

本文记录 HybRAG 作为 VOS App 安装后，外部系统如何在不使用 admin 账号的情况下访问 RAG 能力，并对客户《VOS API 整合需求與產品架構討論稿 V1.0》的要求做覆盖说明。

验证环境：

- VOS 主机：`192.168.1.229`
- HybRAG 版本：`0.1.45`
- HybRAG API：`http://192.168.1.229:29081/api/v1`
- HybRAG 前端：`http://192.168.1.229:29080`
- VOS 登录接口示例：`http://192.168.1.229:8105`
- 测试身份：VOS 普通用户

`29080` 和 `29081` 是 HybRAG VOS App 模板默认暴露的端口。`8105` 只是 229 测试环境中用于命令行登录 VOS 的平台后端端口示例，不属于 HybRAG App 对外端口；正式集成应优先使用 VOS/OAuth 登录流程取得 access token。

文档中的 token、API Key 和密码均使用占位符。真实凭证不要写入脚本仓库、日志或交付文档。

## 结论

HybRAG 目前支持两条外部调用路径：

1. 外部系统先完成 VOS 登录或 OAuth 授权，拿到普通用户的 VOS access token，然后以 `Authorization: Bearer <VOS_ACCESS_TOKEN>` 直接调用 HybRAG API。HybRAG 会校验该 VOS token，并按映射后的 HybRAG 用户权限执行。
2. 普通用户在 HybRAG 「API 集成」里创建自己的个人 API Key，然后外部系统以 `X-API-Key: <API_KEY>` 调用 HybRAG API。个人 API Key 只能使用受限能力和绑定的知识库范围。

不需要外部系统长期持有 admin 账号。普通用户也不需要让 admin 代发个人 API Key。

## 链路一：VOS 登录后直接调用 HybRAG

### 1. 获取 VOS access token

命令行验证可使用 VOS 登录接口：

```bash
VOS_BASE="http://192.168.1.229:8105"
VOS_USER="<普通用户名>"
VOS_PASS="<普通用户密码>"

VOS_TOKEN="$(curl -sS -X POST "$VOS_BASE/v1000/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$VOS_USER\",\"password\":\"$VOS_PASS\"}" \
  | jq -r '.data.access_token')"
```

正式外部应用如果已经走 VOS OAuth 授权码流程取得 access token，可直接复用该 token，不需要用户名密码登录。

### 2. 使用 VOS token 调 HybRAG

```bash
BASE="http://192.168.1.229:29081/api/v1"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

列出当前用户可见知识库：

```bash
curl -sS "$BASE/knowledge-bases?creator=all&page=1&page_size=20" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

创建会话：

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部 API 测试"}' \
  | jq -r '.data.id')"
```

指定知识库发起 RAG 问答：

```bash
KB_ID="<knowledge_base_id>"

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"请用一句话概括这份知识库的重点\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

无会话检索：

```bash
curl -sS -X POST "$BASE/knowledge-search" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"测试知识库重点\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq
```

## 链路二：直接使用个人 API Key 调用 HybRAG

普通用户先在 HybRAG 「设置 -> API 集成」创建个人 API Key，并绑定允许访问的知识库。外部服务端保存该 Key 后即可长期调用。

```bash
BASE="http://192.168.1.229:29081/api/v1"
API_KEY="<普通用户个人 API Key>"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY" | jq
```

创建会话：

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部 API Key 测试"}' \
  | jq -r '.data.id')"
```

指定知识库检索：

```bash
KB_ID="<knowledge_base_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"测试知识库重点\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq
```

如需限定到某一份或几份文档，使用 `knowledge_ids`。这里的 `knowledge_id` 就是业务语义里的文档 ID：上传文件、导入 URL 或创建手动知识后，响应里的 `data.id` 即为该文档 ID；也可以从知识库文档列表接口返回的每条记录 `id` 获取。

```bash
KNOWLEDGE_ID="<knowledge_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"只检索这份文档里的内容\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"]
  }" | jq
```

指定知识库问答：

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"请用一句话概括这份知识库的重点\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## 229 实测结果

| 测试项 | 结果 | 说明 |
| --- | --- | --- |
| VOS 普通用户登录 | 通过 | `POST /v1000/user/login` 返回 access token |
| VOS token 调 HybRAG 当前用户 | 通过 | `GET /auth/me` 返回映射后的 HybRAG 用户 |
| VOS token 列知识库 | 通过 | 返回当前普通用户可见知识库 |
| VOS token 创建会话 | 通过 | `POST /sessions` 返回会话 ID |
| VOS token 知识检索 | 通过 | `POST /knowledge-search` 返回 `200` |
| VOS token RAG 问答 | 通过 | `POST /knowledge-chat/:session_id` 返回 SSE，并出现完成事件 |
| 个人 API Key 当前用户 | 通过 | `GET /auth/me` 返回 Key 绑定用户 |
| 个人 API Key 创建会话 | 通过 | `POST /sessions` 返回会话 ID |
| 个人 API Key 知识检索 | 通过 | `POST /knowledge-search` 返回 `200` |
| 个人 API Key RAG 问答 | 通过 | `POST /knowledge-chat/:session_id` 返回 SSE，并出现完成事件 |
| 普通用户创建高权限 API Key | 已拒绝 | 请求 `manage_members` 返回 `403` |

本次测试知识库没有命中文档分块，因此检索结果条数为 `0`，但接口认证、知识库范围校验、会话创建和 SSE 生成链路均已通过。若要验证 citation/source 的内容结构，需要使用包含已解析文档并能命中查询的知识库。

## 客户需求覆盖

| 需求 | 当前覆盖情况 | 说明 |
| --- | --- | --- |
| 普通用户外部访问自己的 RAG | 已覆盖 | 支持 VOS token 和个人 API Key 两种方式 |
| 不长期使用 admin 账号 | 已覆盖 | 普通用户可自建个人 API Key；VOS token 也继承普通用户身份 |
| 指定 Knowledge Base / KB ID | 已覆盖 | `knowledge_base_ids` 支持一个或多个知识库 ID |
| 指定 Document / Knowledge ID | 应用侧已有 API | 请求字段为 `knowledge_ids`；文档 ID 对应 HybRAG 的 `knowledge_id` |
| Conversation / Session ID | 已覆盖 | `POST /sessions` 创建，`/knowledge-chat/:session_id` 复用 |
| Authentication 方法 | 已覆盖 | `X-API-Key` 与 `Authorization: Bearer <VOS_ACCESS_TOKEN>` |
| RAG Query / Chat API | 已覆盖 | `/knowledge-chat/:session_id`，SSE 输出 |
| Retrieval API | 已覆盖 | `/knowledge-search`，返回检索分块列表 |
| Streaming | 已覆盖 | RAG Chat 使用 SSE |
| Citation / Sources | 接口支持，当前样本未命中 | SSE `references` 事件和检索结果包含来源字段；本次测试 KB 查询未命中文档，未覆盖真实 citation 内容 |
| Knowledge Base CRUD | 应用侧已有 API | 见 `knowledge-base.md`；本次链路测试未创建/删除真实业务 KB，避免污染用户数据 |
| Document ingest / parse / re-index | 应用侧已有 API | 上传、重新解析、批量重新解析、取消解析见 `knowledge.md` |
| KB ACL / 分享 | 应用侧已有部分能力 | 组织共享、viewer/editor 权限见 `organization.md`；不等同于 NAS ACL |
| API Key create/list/update/delete | 已覆盖主要链路 | 创建由 UI 完成，本次验证了 Key 调用和高权限拒绝；接口支持列表、更新范围/过期时间、删除 |
| API Key scope | 已覆盖 | 个人 Key 必须绑定可访问知识库，且只允许 `retrieve`、`chat`、`ingest` |
| API Key revoke | 应用侧支持 | 删除 API Key 即撤销 |
| API Key rotate | 通过重建实现 | 当前没有独立 rotate endpoint；做法是创建新 Key，切换调用方后删除旧 Key |
| API Key expiry | 应用侧支持 | 创建/更新 API Key 可传 `expires_at_unix` |
| NAS Share / Folder / ACL | HybRAG 应用侧不覆盖 | 属于 VOS/NAS 平台 API；HybRAG 只负责已发布到知识库后的检索与问答权限 |
| NAS -> RAG Publish / Unpublish | 部分覆盖 | HybRAG 支持文档上传、删除、重新解析；从 NAS 文件夹选择并发布到 KB 的完整 Portal 工作流需要 VOS/NAS API 配合 |
| User / Group / Role / Permission API | HybRAG 应用侧不覆盖 | 属于 VOS 身份与权限平台；HybRAG 可继承 VOS token 映射后的用户身份 |
| Model Hub list / status | 部分覆盖 | HybRAG 可管理自身模型配置；Model Hub 运行状态属于 Model Hub 应用 API |
| Health | 部分覆盖 | HybRAG 有系统能力和服务状态接口；VOS/Model Hub 全局健康状态需对应平台 API |
| Audit log | 部分覆盖 | HybRAG 有系统审计日志页面/API；VOS/NAS/Model Hub 全局审计需要平台侧支持 |

## 集成建议

- 企业 Portal 或 imShare Server 后端优先使用个人 API Key 链路：实现简单、适合服务端长期保存，且可以按知识库和能力限制权限。
- 需要严格继承当前 VOS 登录用户时，使用 VOS OAuth / 登录得到的 access token，直接作为 `Authorization: Bearer` 调 HybRAG。
- RK3288 等终端设备不要保存 admin、平台级 API Key 或 VOS 用户密码。建议由业务后端保存受限个人 API Key，终端只持有设备凭证。
- NAS 原始文件权限和 RAG 知识库权限要分开设计：NAS 文件可读不代表自动进入 RAG；进入 RAG 前应有 Publish / Approval 流程。
- 客户要求的 VOS User/Group/Role、NAS ACL、全局 audit/health 属于 VOS 平台能力，HybRAG 可以消费身份和承载知识库 API，但不能单独替代 VOS 平台 API。
