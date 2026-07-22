# HybRAG VOS 应用说明

本目录是 ictrek 维护 HybRAG 的唯一当前部署入口。HybRAG 不再维护独立 compose 部署流程，只作为 VOS app `com.ictrek.hybrag` 打包、安装和升级。

当前只发布 pull 模式安装包：本地 `update_version.sh` 只创建触发 tag，GitHub Actions 负责读取飞书和依赖 release、打包并发布正式 release。

## 文档结构

| 路径 | 状态 | 用途 |
| --- | --- | --- |
| `README.md` | 当前维护 | VOS app 打包、发布、依赖、模型、安装和排错主入口。 |
| `src/README.zh-CN.md` / `src/README.en.md` | 当前维护 | 打进 VOS 安装包的简版说明。 |
| `docs/USERGUIDE.md` | 当前维护 | 面向使用者的知识库、文档解析、问答、模型配置和界面操作说明。 |
| `docs/build-images.md` | 当前维护 | HybRAG 四个自有镜像的远端构建、推送和飞书记录规则。 |
| `docs/vos-ollama-prewarm.md` | 当前维护 | Model Hub QA/VLM、embedding 预热和 Gateway 排错。 |
| `docs/upstream-sync.md` | 当前维护 | 合并 Tencent 上游和 ictrek 本地改动的流程。 |
| `docs/legacy/` | 只读备查 | 旧独立部署、旧远程 compose、旧 vLLM/Ollama 手工部署资料，不再按新版本持续更新。 |

仓库旧路径 `docs/ictrek/` 只保留跳转说明，不再新增内容。

## 打包

正式发布入口是 `scripts/update_version.sh`。它只负责自增 `VERSION`、提交版本 commit、创建并推送 `vos-hybrag-v${VERSION}` 触发 tag；GitHub Actions 收到 tag 后会读取飞书组件版本、生成 pull 包并发布 release。

本地 `package.sh` 只用于调试模板或手动验证。未设置 `PACKAGE_VERSION` 时读取当前 `ictrek.app/VERSION`，CI 会显式传入 tag 中解析出的 `PACKAGE_VERSION`。

```bash
cd apps/WeKnora/ictrek.app
./scripts/package.sh
```

脚本会生成一个 pull 模式安装包：

```text
dist/hybrag_${VERSION}_pull.tar
```

安装包内只有 `app.tar.gz`，不会内置镜像归档。脚本会优先读取 `~/.feishu.components.json`，失败时回退到 `~/.feishu.json`，从飞书发布表读取 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 的最新镜像版本，并写入包内 `.env`。这里仍读取 `weknora*` 镜像列，是因为本次只改 VOS 应用、容器和显示名称，不改已发布镜像仓库名。

打包脚本会校验 VOS 入口契约：`routers.yml` 必须声明 `entry-point: true` 和 `embed: true`，`docker-compose.yml` 必须把顶层文档请求 `/app/com.ictrek.hybrag/` 重定向到 VOS 侧边栏内部路径。缺少这些字段时，VOS“我的应用”卡片的“打开”按钮可能只打开空白页或不能在侧边栏打开。

前端镜像还必须能在 VOS 子路径 `/app/com.ictrek.hybrag/` 下运行。HybRAG 前端构建使用相对静态资源路径，并在运行时按当前 URL 注入 base；API 请求、Vue Router history 和登录跳转都会复用同一个 base。这样同一个 `weknora-ui` 镜像既能在普通根路径部署，也能被 VOS iframe 作为 `/app/com.ictrek.hybrag/` 页面打开。若以后修改 `frontend/index.html`、`frontend/embed.html`、`frontend/vite.config.ts` 或 `frontend/src/utils/app-base.ts`，需要在 VOS 实机中验证 `/app/com.ictrek.hybrag/` 下的 `assets`、`config.js`、`tdesign-icons` 和 `/api/v1/auth/vos-sso` 均不再落到 VOS 根路径 404。

脚本还会解析 `manifest.yml`、`configs.yml`、`routers.yml` 和 `docker-compose.yml`，防止生成 YAML 语法错误的安装包。`manifest.yml` 必须声明 VOS 安装 UI 使用的 `profiles`，profile 名只能使用小写字母、数字和连字符，并且必须与 `docker-compose.yml` 的 compose profile 完全一致；飞书 sheet 名只允许出现在打包脚本的映射关系中。如果运行环境有 Docker Compose，会额外对 `amd`、`arm` 两个 profile 执行 `docker compose config`，提前发现未展开镜像变量、profile 服务缺失或 compose 语法问题。

## 安装与升级

安装前先确认 VOS 中已经安装并运行：

- `com.ictrek.model-hub`：提供 `model-hub-ollama-qa` 和 `model-hub-ollama-embedding` 两个服务。
- `com.ictrek.pgv`：提供 `shared-pgv:5432` PostgreSQL/pgvector 服务。

HybRAG 安装包不启动 Model Hub 和 PGV，也不启动自己的 Ollama。安装包只包含 HybRAG app、frontend、docreader、sandbox、Redis 和 Neo4j。安装时选择 `amd` 或 `arm` profile，VOS 会按包内 `.env` 拉取飞书表中记录的四个 HybRAG 镜像。

升级时不要手工复用旧独立部署 compose，也不要把 `docs/legacy/deploy-template/` 当成当前安装模板。VOS app 的最终 compose 只来自 `ictrek.app/src/docker-compose.yml` 经打包流程渲染出的安装包。

旧持久化数据可以继续使用。历史版本如果在数据库里留下 `hybrag-ollama-qa` 或 `hybrag-ollama-embedding` 模型地址，新 app 镜像启动迁移时会把内置模型行修正到 Model Hub 的 `11535/v1` Gateway。只有启动过包含该迁移的新 app 镜像后，旧数据库中的这类残留才会被修复。

VOS 中打开 HybRAG 时，前端会走 VOS SSO。当前临时方案从 VOS 页面上下文取得用户信息，后端校验 VOS bearer token 后自动创建或登录 `${username}@local`；`admin` 用户对应 `admin@local`，并拥有系统管理员权限。后续 VOS 改为标准 OIDC 或 iframe 注入用户信息时，只需要替换该身份入口，不应恢复独立登录流程作为 VOS 默认路径。

## 其他 VOS App 以当前用户身份接入 HybRAG

HybRAG 同时保留两类认证方式：

- API Key：用于外部系统、服务端任务、自动化脚本和不具备 VOS 用户上下文的集成。
- VOS token exchange：用于其他 VOS app 在已登录 VOS 的前提下，以“当前 VOS 用户”的身份访问 HybRAG，不需要给用户暴露或保存 HybRAG API Key。

其他 VOS app 如果要“当前打开应用的 VOS 用户是谁，就访问 HybRAG 中对应用户的空间”，应使用 VOS token exchange，不要直接伪造 `X-External-User-ID`。`X-External-User-ID` 只适合可信服务端在已经持有 HybRAG API Key 的情况下做终端用户隔离；它不是免 key 用户登录机制。

### 身份映射规则

HybRAG 后端收到 VOS access token 后，会调用 `HYBRAG_VOS_USERINFO_URL` 指向的 VOS `/v1000/user/check` 校验 token。校验成功后按以下规则处理：

1. 读取 VOS 用户名。
2. 映射为 HybRAG 本地账户 `${username}@local`。
3. 如果账户不存在，自动创建账户。
4. 如果个人空间不存在，按 `WEKNORA_AUTH_DEFAULT_TENANT_MODE=create_personal` 自动创建 `${username}'s Workspace`。
5. 返回 HybRAG 自己签发的 `token` 和 `refresh_token`。
6. 后续 HybRAG API 调用使用 `Authorization: Bearer <HybRAG token>`。

特殊规则：

- VOS `admin` 映射为 `admin@local`。
- `admin@local` 会自动提升为 HybRAG 系统管理员，并拥有跨空间管理能力。
- 普通 VOS 用户只拥有其 HybRAG 个人空间内的权限，除非后续由管理员把他加入其他空间或组织。

### 接口

推荐其他 VOS app 调用：

```http
POST /api/v1/auth/vos-token-exchange
Authorization: Bearer <VOS access token>
```

也可以使用 JSON body，便于没有统一 header 封装的调用方：

```http
POST /api/v1/auth/vos-token-exchange
Content-Type: application/json

{
  "access_token": "<VOS access token>"
}
```

为兼容 HybRAG 自身前端，旧入口仍保留：

```http
POST /api/v1/auth/vos-sso
```

`/api/v1/auth/vos-sso` 与 `/api/v1/auth/vos-token-exchange` 当前共用同一套逻辑。新 VOS app 建议使用语义更明确的 `/api/v1/auth/vos-token-exchange`。

成功响应与普通登录一致，关键字段如下：

```json
{
  "success": true,
  "data": {
    "token": "<HybRAG access token>",
    "refresh_token": "<HybRAG refresh token>",
    "user": {
      "username": "admin",
      "email": "admin@local"
    },
    "tenant": {
      "id": 10000,
      "name": "admin's Workspace"
    },
    "memberships": [
      {
        "tenant_id": 10000,
        "tenant_name": "admin's Workspace",
        "role": "owner"
      }
    ]
  }
}
```

其他 VOS app 不应长期保存 VOS access token。建议只在需要访问 HybRAG 时用当前 VOS token 换一次 HybRAG token，然后短期缓存 HybRAG token；HybRAG token 过期后再重新 exchange，或用响应里的 `refresh_token` 调 HybRAG `/api/v1/auth/refresh`。

### 其他 app 如何取得当前 VOS 用户

推荐接入方式是“其他 VOS app 后端转发当前 VOS token”：

1. 用户在 VOS 中打开其他 app。
2. 该 app 从 VOS 当前会话中取得当前用户的 VOS access token。
3. 该 app 后端把这个 VOS access token 传给 HybRAG `/api/v1/auth/vos-token-exchange`。
4. HybRAG 调 VOS `/v1000/user/check` 校验 token，并以 VOS 返回的用户名作为唯一身份来源。
5. HybRAG 自动映射或创建 `${username}@local` 账户和个人空间。
6. 该 app 后续调用 HybRAG API 时使用 exchange 返回的 HybRAG token。

这样可以保证多个 VOS app 看到的是同一个用户身份：

| VOS 当前用户 | HybRAG 账户 | HybRAG 默认空间 |
| --- | --- | --- |
| `admin` | `admin@local` | `admin's Workspace` |
| `alice` | `alice@local` | `alice's Workspace` |
| `zhangsan` | `zhangsan@local` | `zhangsan's Workspace` |

当前 VOS 还没有稳定的标准 OIDC 或正式 iframe 注入协议时，HybRAG 前端使用一套临时兼容顺序。其他 app 如需在前端直接发起 exchange，可以按相同顺序读取 token：

1. 优先读取 `window.__VOS_APP_CONTEXT__.accessToken`。
2. 其次读取 `window.__VOS_APP_CONTEXT__.token`。
3. 再读取 `window.__VOS_ACCESS_TOKEN__`。
4. 如果这些注入值都不存在，再兼容读取 VOS 同源 `localStorage` 中以 `-core-access` 结尾的 store，例如 `core-access`、`VIVIBIT-core-access`。
5. 如果 store 使用 `secure-ls` 加密，则需要用 VOS 当前约定的加密 key 解出 access token。HybRAG 兼容环境变量 `VITE_VOS_STORE_SECURE_KEY`，未配置时使用当前默认值。

这套 localStorage/secure-ls 读取方式只是过渡方案。新的 VOS app 开发时，优先让自己的后端或 VOS 官方 SDK 提供当前用户的 VOS access token；未来 VOS 提供 OIDC 或正式 iframe 注入后，应切换到官方方式，HybRAG 的 token exchange 入口不用变。

### 调用示例

假设其他 VOS app 的后端可以拿到当前用户的 VOS access token：

```bash
HYBRAG_BASE="http://hybrag-app:8080"
VOS_TOKEN="<current VOS access token>"

HYBRAG_TOKEN="$(
  curl -sS -X POST "${HYBRAG_BASE}/api/v1/auth/vos-token-exchange" \
    -H "Authorization: Bearer ${VOS_TOKEN}" \
    | jq -r '.data.token'
)"

curl -sS "${HYBRAG_BASE}/api/v1/auth/me" \
  -H "Authorization: Bearer ${HYBRAG_TOKEN}"
```

在 VOS `vos_default` 网络内，其他 app 应优先通过 HybRAG app 服务名访问 HybRAG 后端。不同 VOS 安装实例的 Compose project 名会变化，不要在其他 app 中写死完整容器名。更稳妥的做法是由 VOS 或 app 安装配置暴露 HybRAG API 地址；当前 HybRAG 前端公开入口仍是 `/app/com.ictrek.hybrag/`，服务端 API 地址需要按实际 VOS 网络和路由配置确定。

如果其他 VOS app 只能在浏览器 iframe 内运行，且当前 VOS 版本没有提供标准 OIDC 或正式的 iframe 用户注入，则可以参考 HybRAG 前端的临时实现：优先读 `window.__VOS_APP_CONTEXT__` / `window.__VOS_ACCESS_TOKEN__`，再兼容读取 VOS 当前同源会话 store。这个兼容方式是过渡方案；未来 VOS 支持标准 OIDC 或直接向 iframe 注入用户信息后，应切换到 VOS 官方方式获取当前用户 token。

### 权限边界

VOS token exchange 返回的是“当前 VOS 用户对应的 HybRAG 用户”的 Bearer token，因此权限与该 HybRAG 用户一致：

- 可以访问该用户自己的空间、模型配置可见项、知识库、会话和被授权资源。
- 不会因为调用方是另一个 VOS app 就自动获得 HybRAG 全局管理员权限。
- `admin@local` 是例外，因为它被明确配置为 HybRAG 系统管理员。
- 如果需要服务端批处理、跨用户管理或绕过用户空间限制，使用 HybRAG API Key，并在 HybRAG「API 集成」中选择合适的能力授权或空间完全访问。

不要把 `X-External-User-ID` 当作免 key 登录：

```http
X-External-User-ID: alice
```

这个 header 只有在请求同时携带有效 HybRAG API Key，并且该空间的“用户身份模式”配置为直接请求头时才有意义。它用于把 API Key 调用产生的会话按终端用户隔离，不负责校验 VOS 用户身份。

### 平台 API Key 迁移修复

平台 API Key 使用 `tenant_api_keys.scope_type` 区分普通空间 Key 和平台 Key。历史数据库如果曾经出现迁移记录已推进、但真实表结构没有 `scope_type` 的情况，进入「平台 API Key」页面会报 `Failed to list platform API keys`，后端日志会出现：

```text
ERROR: column "scope_type" does not exist
```

新版本 app 镜像内置 `000075_tenant_api_key_scope_repair` 兼容迁移，会在启动迁移时自动补齐 `scope_type` 字段、`tenant_id` 可空约束、检查约束和索引。以后遇到同类旧库，不要只手工改数据库；应先确认已经运行包含该迁移的新 app 镜像。如果现场急需恢复页面，可以按该迁移文件里的 SQL 临时补库，随后仍要升级到包含该迁移的镜像。

## Profiles

HybRAG 自身不再启动 Ollama，Model Hub 负责 GPU/无 GPU、L4T、Thor 等运行时差异，因此本应用只发布 2 个 profile。

| profile | 飞书 sheet | 说明 |
| --- | --- | --- |
| `amd` | `AMD_with_cuda` | x86_64 / AMD 通用 HybRAG |
| `arm` | `ARM_with_cuda` | ARM 通用 HybRAG |

安装时由 VOS 指定其中一个 profile。手动检查 compose 时也必须只启用一个 profile：

```bash
docker compose --profile amd config
docker compose --profile arm config
```

## 资源默认值

默认模型引用 Model Hub 的两个预热 Ollama 运行时：QA/VLM 模型为 `qwen3.5:2b`，embedding 模型为 `bge-m3`。HybRAG 默认按 Model Hub 分离运行时配置：

| 资源项 | 默认值 | 含义 |
| --- | ---: | --- |
| Model Hub QA/VLM 总槽位 | `8` | 由 Model Hub QA Ollama 提供；HybRAG 侧按 8 个主模型槽位调度。 |
| QA/VLM 聊天预留 | `2` | `WEKNORA_CHAT_RESERVED_CONCURRENCY=2`，后台任务最多共享剩余 `6` 个主模型槽位。 |
| 后台主模型共享槽位 | `6` | `WEKNORA_MODEL_MAX_CONCURRENCY=6`，Graph/Wiki/VLM/摘要/问题生成等后台调用共用。 |
| QA 上下文 | `24000` | 应用侧 `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS=24000`；Model Hub QA 默认上下文为 `24576`。 |
| QA 输入/输出预算 | `16000 / 8000` | `WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS=0`、`WEKNORA_*MAX*_TOKENS=8000`。 |
| Model Hub Embedding 总槽位 | `4` | 由 Model Hub embedding Ollama 提供；HybRAG 文档 embedding 默认只使用 2 个。 |
| 文档 embedding 槽位 | `2` | `CONCURRENCY_POOL_SIZE=2`，另外约 `2` 个槽位留给聊天检索。 |

## 依赖和模型

`manifest.yml` 源码模板只声明依赖 `com.ictrek.model-hub >=0.0.29` 和 `com.ictrek.pgv` 的最低基线；正式打包时，GitHub Actions 会查询依赖 release，把最终安装包里的依赖版本自动更新为当前最新可用版本。`docker-compose.yml` 不启动 model_hub 或 Postgres 服务。Model Hub 提供独立的 QA 与 embedding Ollama 预热运行时，当前 HybRAG 需要使用同时兼容 OpenAI `/v1/*` 与 Ollama `/api/*` 的 `11535` gateway。HybRAG 包内只启动自身服务、Redis 和 Neo4j；Postgres 通过 PGV 在 `vos_default` 网络上的 `shared-pgv:5432` 访问，模型调用通过 Model Hub 暴露的两个 gateway。

PGV 文档中默认预置给 WeKnora/HybRAG 使用的连接信息是：

```text
DB_HOST=shared-pgv
DB_PORT=5432
DB_USER=weknora
DB_PASSWORD=weknora
DB_NAME=WeKnora
```

这里数据库名和用户仍使用 `weknora/WeKnora`，是为了兼容 PGV 默认初始化结果；VOS 应用显示名、容器名和 app id 改为 HybRAG 不要求改数据库名。安装 UI 会暴露 `WEKNORA_DB_HOST`、`WEKNORA_DB_PORT`、`WEKNORA_DB_USER`、`WEKNORA_DB_PASSWORD`、`WEKNORA_DB_NAME`，如果 PGV 安装时改过用户、密码或数据库名，在安装 HybRAG 时同步改这些值即可。

HybRAG 不再启动自己的 Ollama 容器，也不再挂载 Model Hub 模型目录。Model Hub 应先安装并运行在同一个 `vos_default` 网络中，并提供两个稳定服务名：

| 用途 | 服务名 | Gateway | 默认模型 |
| --- | --- | --- | --- |
| QA / VLM | `model-hub-ollama-qa` | `http://model-hub-ollama-qa:11535/v1` | `qwen3.5:2b` |
| Embedding | `model-hub-ollama-embedding` | `http://model-hub-ollama-embedding:11535/v1` | `bge-m3` |

模型下载、预热、常驻、上下文和 Ollama 并发由 Model Hub 安装配置负责。HybRAG 安装 UI 不再暴露 Ollama 模型名和 gateway 地址；如果 Model Hub 修改了服务名或端口，需要同步修改 HybRAG 包模板或运行后在 UI 中手动调整模型行。

HybRAG 默认模型行必须指向 Model Hub Ollama Gateway，也就是 `http://<ollama-service>:11535/v1`。不要把 QA、VLM、embedding 或 `OLLAMA_BASE_URL` 配到原生 Ollama `11434`，否则请求不会经过 Gateway，Model Hub 看不到槽位、阶段、token/s 等统计信息。VOS 包内默认 `OLLAMA_BASE_URL=http://model-hub-ollama-qa:11535`，模型行默认使用带 `/v1` 的 gateway 地址。

VOS 安装包不会放额外 `config/` 目录。App 容器启动脚本会在运行时生成 `builtin_models.yaml`，自动创建三条 YAML 托管模型行，并在界面里用 `display_name` 区分两个 Ollama 后端：

| 类型 | display_name | endpoint |
| --- | --- | --- |
| KnowledgeQA | `Model Hub Ollama QA (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` |
| VLLM | `Model Hub Ollama VLM (model-hub-ollama-qa)` | `http://model-hub-ollama-qa:11535/v1` |
| Embedding | `Model Hub Ollama Embedding (model-hub-ollama-embedding)` | `http://model-hub-ollama-embedding:11535/v1` |

这些模型行不写在镜像里，也不随 VOS 包以目录形式挂载；当前 VOS parser 只接受固定顶层文件，包内不要加入 `config/`。`name` 固定为 `qwen3.5:2b` 和 `bge-m3`，endpoint 固定指向 Model Hub Gateway。运行后也可以在 HybRAG UI 中添加或修改其他模型；如果管理员手动接管某条 YAML 模型行，需要清空该行的 `managed_by`，否则后续安装包升级会按 YAML 继续同步。

Ollama Qwen3.5 关闭思考使用 `extra_config.thinking_control=think`，请求会发送顶层 `think:false`。vLLM / generic Qwen3.5 后端关闭思考使用 `extra_config.thinking_control=chat_template_kwargs`，请求会发送 `chat_template_kwargs.enable_thinking=false`。两者不要混用。

Model Hub 预热、常驻和 Gateway 检查见 [docs/vos-ollama-prewarm.md](docs/vos-ollama-prewarm.md)。用户界面操作见 [docs/USERGUIDE.md](docs/USERGUIDE.md)。

## 版本更新与 Release

`scripts/update_version.sh` 用于发布自增版本并触发 GitHub Actions。它不是 dry-run；执行成功后会修改版本文件、提交 commit、创建 `vos-hybrag-v${VERSION}` 触发 tag，并推送分支和 tag。真正的依赖版本查询、飞书查表、pull 包打包、release notes 生成和 tar 上传由 `.github/workflows/vos-release.yml` 完成。

```bash
./scripts/update_version.sh patch
```

可选参数：

| 参数 | 行为 |
| --- | --- |
| `patch` | `0.0.1 -> 0.0.2`，默认值 |
| `minor` | `0.0.1 -> 0.1.0` |
| `major` | `0.0.1 -> 1.0.0` |

脚本会：

1. 自增 `ictrek.app/VERSION`。
2. 提交 `VERSION`，提交信息为 `chore: release VOS hybrag ${VERSION}`。
3. 创建并推送 `vos-hybrag-v${VERSION}` 触发 tag。
4. GitHub Actions 收到 tag 后执行 `.github/workflows/vos-release.yml`。

GitHub Actions 会：

1. 使用 `VOS_DEPENDENCY_RELEASE_TOKEN` 查询 `model_hub_*_pull.tar` 与 `pgv_*_pull.tar` 的最新版本，并写入 CI 工作区内的 `manifest.yml`。
2. 使用 `FEISHU_APP_ID`、`FEISHU_APP_SECRET` 和可选 `FEISHU_SPREADSHEET_TOKEN` 写出 `~/.feishu.components.json`。
3. 调用 `scripts/package.sh`，从飞书发布表读取 `weknora*` 四镜像的最新版本，生成 `dist/hybrag_${VERSION}_pull.tar`。
4. 生成 release notes。
5. 创建公开 release tag `v${VERSION}`，并上传 pull 模式 tar 包。`vos-hybrag-v${VERSION}` 只用于触发 CI，不作为公开 release tag。

执行前检查：

```bash
cd apps/WeKnora
git status --short
git remote get-url origin
git fetch --tags origin
```

要求：

- HybRAG 工作区必须干净；脚本会在存在未提交改动时退出。
- `origin` 应指向发布目标仓库，例如 `git@github.com:ictrektech/WeKnora.git`。
- 本地只需要能向 HybRAG push 分支和 tag；不需要本地读取飞书，也不需要本地创建 GitHub Release。
- GitHub Actions 需要能读取依赖 release、读取飞书发布表，并能写 HybRAG release。

GitHub secrets：

| Secret | 用途 | 建议配置位置 |
| --- | --- | --- |
| `VOS_DEPENDENCY_RELEASE_TOKEN` | 读取同组织私有依赖仓库 release assets，例如 `model_hub`、`pgv` | Organization secret，`Repository access` 可选 `All repositories`，权限 `Contents: Read-only` |
| `FEISHU_APP_ID` | 飞书应用 ID，用于读取镜像发布表 | Organization secret；HybRAG 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_APP_SECRET` | 飞书应用 secret | Organization secret；HybRAG 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_SPREADSHEET_TOKEN` | 可选；覆盖默认飞书表 token | Organization secret 或 repository secret |

HybRAG 是 public repo，因此当前组织级 Feishu secrets 可被 GitHub Actions 读取。其他没有私有依赖 release 的 VOS app 可继续沿用各自现有流程，不需要套用 HybRAG 的依赖 token 逻辑。

## 路由入口

`routers.yml` 使用固定的 group/page 入口。真实页面作为 VOS iframe 页面加载，并保留 `entry-point: true` 和 `embed: true`。为兼容仍读取 `frontend_base_path` 的旧“打开”按钮，Compose/Traefik 会把顶层文档请求 `/app/com.ictrek.hybrag/` 重定向到 VOS hash；iframe 请求继续进入真实应用页面，不会被重定向。

HybRAG 的固定入口契约是：

- `app id`: `com.ictrek.hybrag`
- `group.id`: `com-ictrek-hybrag`
- `page.id`: `hybrag`
- `iframe-src`: `/app/com.ictrek.hybrag/?v=${VERSION}`
- VOS 内部侧边栏路径：`#/app/com.ictrek.hybrag/com-ictrek-hybrag/hybrag`

`scripts/package.sh` 会在生成 `app.tar.gz` 后校验以上字段；不匹配时直接失败。新增或修改入口时必须同步更新模板和脚本校验值。

当前这条说明里的“其他 VOS app”包括 `model_hub`、`pgv`、`motrix-next`、`cc_setup`。这些 app 暂不因为 HybRAG 的私有依赖查询需求改变发布流程。

发布命令：

```bash
cd apps/WeKnora/ictrek.app
./scripts/update_version.sh patch
```

发布后验证：

```bash
VERSION="$(cat VERSION)"
gh run list --repo ictrektech/WeKnora --workflow vos-release.yml --limit 1
gh release view "v${VERSION}" --repo ictrektech/WeKnora \
  --json tagName,targetCommitish,url,assets
git tag --list "vos-hybrag-v${VERSION}" "v${VERSION}" --format='%(refname:short) %(objectname:short)'
```

如果脚本失败，按阶段处理：

- 本地脚本失败：通常是工作区不干净、版本号非法、触发 tag 或公开 release tag 已存在。先用 `git status --short`、`git tag --list 'vos-hybrag-v*' 'v*'` 检查。
- CI 依赖 release 查询失败：检查 `VOS_DEPENDENCY_RELEASE_TOKEN` 是否可用，是否有同组织仓库 `Contents: Read-only` 权限。
- CI 飞书查表失败：检查 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_SPREADSHEET_TOKEN`，以及目标 profile 的 sheet 里是否存在 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 列，并且最新行有 tag。
- CI release 创建失败：查看 `VOS Pull Package Release` workflow 日志。若 package 已生成但 release 未创建，可在本地确认后补执行：

```bash
VERSION="$(cat VERSION)"
gh run view --repo ictrektech/WeKnora --log
gh release view "v${VERSION}" --repo ictrektech/WeKnora
```

## GitHub Actions 依赖查询验证

本机 `gh` 能查到私有仓库不代表 GitHub Actions 默认 `GITHUB_TOKEN` 也能查到。`Check VOS Dependency Release Access` workflow 用于验证 CI 能否读取 VOS 依赖仓库 release assets。

HybRAG 仓库需要配置名为 `VOS_DEPENDENCY_RELEASE_TOKEN` 的 repository 或 organization secret。推荐使用 fine-grained PAT：

- `Repository access`: `All repositories`
- `Permissions`: 只添加 `Contents: Read-only`

配置后手动运行 `Check VOS Dependency Release Access`。日志中应出现类似：

```text
Using VOS_DEPENDENCY_RELEASE_TOKEN for dependency release lookup.
ictrektech/model_hub: latest visible pull asset version is 0.0.17
ictrektech/pgv: latest visible pull asset version is 0.0.13
```

CI 发布流程应复用同一个 secret 作为 `GH_TOKEN`，不要依赖当前仓库默认 `GITHUB_TOKEN` 读取其他私有仓库。
