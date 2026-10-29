# HybRAG VOS 应用说明

本目录是 ictrek 维护 HybRAG 的唯一当前部署入口。HybRAG 不再维护独立 compose 部署流程，只作为 VOS app `com.ictrek.hybrag` 打包、安装和升级。

开发分支的上游整合、兼容取舍及验证范围见[上游同步记录](docs/upstream-sync.md)。源码合并后仍须完成镜像与 VOS 包发布，已安装应用才会获得更新。

源码已合入空间默认模型设置：在「设置 → 默认设置」配置各用途模型；显式选择的资源模型优先，清空时恢复部署默认回退。用法见[默认模型](../website-docs/03-features/06-models.md#workspace-defaults)，新增迁移编号及隔离升级结果见[本轮同步记录](docs/upstream-sync.md#2026-10-29-上游整合记录)。

当前只发布 pull 模式安装包：本地 `update_version.sh` 负责更新版本、创建 release commit 和触发 tag，GitHub Actions 负责读取飞书和依赖 release、打包并发布正式 release。需要把 VOS changelog 随版本一起提交时，先用 `$cl` 准备并审核 `ictrek.app/CHANGELOG.md`，再使用 `--with-changelog`。

后端镜像在 appuser 与 root 的默认 DuckDB 缓存目录均预装同版本 spatial/excel 扩展。VOS root 模式启动不依赖外网下载扩展，也不需要禁用 DuckDB 或 XLSX 能力。

系统设置的“版本信息”会同时显示代码构建版本、VOS App 版本以及当前 profile 使用的 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 镜像 tag。VOS App 版本由 `package.sh` 将 `__APP_VERSION__` 渲染为发布版本；四个镜像引用由 GitHub Actions 打包时从飞书读取并写入 compose，随后作为环境变量传给后端。普通非 VOS 部署不设置这些变量时不会显示对应行，也不影响启动。

## 文档结构

| 路径 | 状态 | 用途 |
| --- | --- | --- |
| `README.md` | 当前维护 | VOS app 打包、发布、依赖、模型、安装和排错主入口。 |
| `src/README.zh-CN.md` / `src/README.en.md` | 当前维护 | 打进 VOS 安装包的简版说明。 |
| `docs/USERGUIDE.md` | 当前维护 | 面向使用者的知识库、文档解析、问答、模型配置和界面操作说明。 |
| `docs/legal-workspace.md` | 当前维护 | 法律工作台的工作区开关、侧栏入口、数据保留和删除边界。 |
| `docs/patent-workspace.md` | 当前维护 | 专利检索与写作指导、独立会话、工作区开关及数据清理边界。 |
| `docs/legal-assistant.md` | 当前维护 | 法务对话助手的使用范围、会话归属、权限和验证边界。 |
| `docs/smart-archive.md` | 当前维护 | 智能档案的导入队列、字段证据、检索、提醒和验证边界。 |
| `docs/build-images.md` | 当前维护 | HybRAG 四个自有镜像的远端构建、推送和飞书记录规则。 |
| `docs/vos-ollama-prewarm.md` | 当前维护 | Model Hub QA/VLM、embedding 下载、预热和原生 Ollama 接口排错。 |
| `docs/local-dev/README.md` | 当前维护 | 源码后端/前端的本地快速调试、隔离基础设施和模型 endpoint 配置。 |
| `docs/upstream-sync.md` | 当前维护 | 合并 Tencent 上游和 ictrek 本地改动的流程。 |
| `docs/legacy/` | 只读备查 | 旧独立部署、旧远程 compose、旧 vLLM/Ollama 手工部署资料，不再按新版本持续更新。 |

仓库旧路径 `docs/ictrek/` 只保留跳转说明，不再新增内容。

## 打包

正式发布入口是 `scripts/update_version.sh`。默认模式只负责自增 `VERSION`、提交版本 commit、创建并推送 `vos-hybrag-v${VERSION}` 触发 tag；使用 `--with-changelog` 时，会校验已准备好的 `ictrek.app/CHANGELOG.md`，并把其中未提交的修改放入同一个 release commit，也允许 changelog 已提交。GitHub Actions 收到 tag 后会读取飞书组件版本、生成 pull 包并发布 release。

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

VOS 运行时版本信息由 compose 按 AMD/ARM profile 自动写入，部署者不需要手工维护：

```text
WEKNORA_VOS_APP_VERSION
WEKNORA_APP_IMAGE
WEKNORA_UI_IMAGE
WEKNORA_DOCREADER_IMAGE
WEKNORA_SANDBOX_IMAGE
```

镜像构建时通过 `COMMIT_ID_ARG` 注入源码 commit；GitHub Actions 也可以使用
`GITHUB_SHA`。远程构建目录通常不包含 `.git`，所以同步源码时必须显式传入
commit 或写入 `.git-commit`，否则系统设置会显示 `(unknown)`。

打包脚本会校验 VOS 入口契约：`routers.yml` 必须声明 `entry-point: true` 和 `embed: true`，`docker-compose.yml` 必须把顶层文档请求 `/app/com.ictrek.hybrag/` 重定向到 VOS 侧边栏内部路径。缺少这些字段时，VOS“我的应用”卡片的“打开”按钮可能只打开空白页或不能在侧边栏打开。

前端镜像还必须能在 VOS 子路径 `/app/com.ictrek.hybrag/` 下运行。HybRAG 前端构建使用相对静态资源路径，并在运行时按当前 URL 注入 base；API 请求、Vue Router history 和登录跳转都会复用同一个 base。这样同一个 `weknora-ui` 镜像既能在普通根路径部署，也能被 VOS iframe 作为 `/app/com.ictrek.hybrag/` 页面打开。若以后修改 `frontend/index.html`、`frontend/embed.html`、`frontend/vite.config.ts` 或 `frontend/src/utils/app-base.ts`，需要在 VOS 实机中验证 `/app/com.ictrek.hybrag/` 下的 `assets`、`config.js`、`tdesign-icons` 和 `/api/v1/auth/vos-sso` 均不再落到 VOS 根路径 404。

脚本还会解析 `manifest.yml`、`configs.yml`、`routers.yml` 和 `docker-compose.yml`，防止生成 YAML 语法错误的安装包。`manifest.yml` 必须声明 VOS 安装 UI 使用的 `profiles`，profile 名只能使用小写字母、数字和连字符，并且必须与 `docker-compose.yml` 的 compose profile 完全一致；飞书 sheet 名只允许出现在打包脚本的映射关系中。如果运行环境有 Docker Compose，会额外对 `amd`、`arm` 两个 profile 执行 `docker compose config`，提前发现未展开镜像变量、profile 服务缺失或 compose 语法问题。

## 安装与升级

安装前先确认 VOS 中已经安装并运行：

- `com.ictrek.model-hub >=0.0.92`：提供 `model-hub-ollama-qa`、`model-hub-ollama-embedding` 和 `model-hub-ollama-rerank` 三个服务；旧版 Model Hub 需先升级，以提供 ReRank（对检索结果重新评分和排序）所需的 `/v1/rerank` 接口。
- `com.ictrek.pgv`：提供 `shared-pgv:5432` PostgreSQL/pgvector 服务。统一源码与发布仓库已迁至 `ictrektech/tabularis`，打包时从该仓库解析 PGV 最新发布版本；应用 ID 与连接参数不变。PGV 新增的共享 Redis 使用 `pgv-redis`，HybRAG 现有的 `hybrag-redis` 连接不在本次源码迁移中自动切换。

HybRAG 安装包不启动 Model Hub 和 PGV，也不启动自己的 Ollama。安装包只包含 HybRAG app、frontend、docreader、sandbox、Redis 和 Neo4j。安装时选择 `amd` 或 `arm` profile，VOS 会按包内 `.env` 拉取飞书表中记录的四个 HybRAG 镜像。

HybRAG 私有数据统一使用 VOS 分配的 `VOS_APP_STORAGE_PATH`，并放入 `files/`、`docreader/`、`redis/`、`neo4j/` 子目录。安装表单不再提供这些宿主机路径，也不允许修改固定的 `/var/run/docker.sock` 挂载。

升级时不要手工复用旧独立部署 compose，也不要把 `docs/legacy/deploy-template/` 当成当前安装模板。VOS app 的最终 compose 只来自 `ictrek.app/src/docker-compose.yml` 经打包流程渲染出的安装包。

PGV 与 HybRAG 分步升级的操作和验证结果见 [ARM VOS 顺序升级验证](#arm-vos-顺序升级验证)。

从旧版路径配置升级时，应先把原 `files`、`docreader`、`redis`、`neo4j` 数据迁移到新版本对应的 VOS 应用存储子目录。历史版本如果在数据库里留下 `hybrag-ollama-qa` 或 `hybrag-ollama-embedding` 模型地址，新 app 镜像启动时会把内置模型行修正到 Model Hub Ollama 原生 `11434/v1` 地址。

VOS 中打开 HybRAG 时，前端优先走 VOS OIDC Fastpath：同域 iframe 内使用 `window.vos_platform.api.v1000.oauth2` 完成 `authorize -> token`，然后把 VOS OIDC 应用 access token 交给 HybRAG 后端；后端调用 `HYBRAG_VOS_OIDC_USERINFO_URL` 指向的 `/v1000/oauth2/userinfo` 校验后，自动创建或登录 `${username}@local`。旧的 `/v1000/user/check` token exchange 仍保留为老 VOS 版本降级路径。`admin` 用户对应 `admin@local`，并拥有系统管理员权限。

HybRAG 当前只按 VOS app 使用，前端不会再展示普通登录/注册入口。若用户打开应用时后端、PGV 或 Model Hub 尚未就绪，登录页会保持 VOS 自动登录等待状态并重试；如果长时间停留在等待页，优先检查 app 容器是否已监听 `8080`、PGV 是否可连接、以及后端日志中的容器初始化步骤。

已实现：`auth.registration_mode` 的 `self_serve`、`invite_register`、`invite_only` 分别允许公开密码注册、仅有效邀请链接密码注册、禁止密码注册；这些档位不限制 VOS 身份验证后的自动开户。

登录必须先成功保存 access token 和 refresh token，任一写入失败都不会返回成功登录；后续 token 查询失败会报告查询错误，不再误报为已撤销。

设置中的「API 文档」入口对普通空间成员可见，无需先创建 API Key。

已实现（前端源码）：「常规设置 → 语言」选择 `Automatic (browser / VOS)`（中文为「自动（浏览器／VOS）」）时，在 VOS 内优先跟随平台语言；手动选择的语言保存在当前浏览器，平台切换不会覆盖它。切回自动时采用最近收到的 VOS 语言。没有 VOS 语言或独立打开时使用浏览器首选语言中的受支持语言，再回退到部署默认和内置默认 `zh-CN`。繁体中文标记为 `zh-TW`，界面采用台湾用语。安装环境需升级包含本次前端改动的镜像后生效。

VOS bridge（平台向同源应用 iframe 注入的接口）使用 `window.vos_platform`。前端等待注入，先通过 `broadcast.on('locale-change', handler)` 订阅，再调用 `getState()` 读取 `state.locale`。事件载荷为 `{ locale: 'en-US' }` 或 `{ locale: 'zh-CN' }`；事件修订计数（已收到事件的次数）阻止较早的异步快照覆盖新事件。语言编码映射到应用支持的 `zh-CN`、`zh-TW`、`en-US`、`ru-RU`、`ko-KR`、`ja-JP`，例如 `en` 映射为 `en-US`，`zh-Hant` 映射为 `zh-TW`；不支持的编码不改变当前语言。

语言切换只更新 i18n（界面翻译状态），不刷新页面或重建表单；组件库文案和后续请求的 `Accept-Language` 同步采用当前语言。旧 VOS 缺少 bridge 时保留同源语言存储兼容；bridge 订阅成功后，旧存储事件不再覆盖平台语言。快照读取失败时保留回退语言，后续有效事件仍可更新；等待注入期间先显示回退语言，因此首屏可能短暂显示浏览器语言。

已验证（2026-10-12，本地）：[语言回归测试](../frontend/src/i18n/vosLocale.test.ts)覆盖延迟注入、快照与事件竞争、手动选择保护、切回自动、读取失败和独立打开降级；[设置组件浏览器测试](../frontend/tests/e2e/vos-locale.spec.ts)用模拟 bridge 验证中英文切换、保留手动日语选择、切回自动及未保存输入不丢失。类型检查、翻译检查和生产构建通过。真实 VOS 登录会话中的平台切换及升级后的线上行为仍待验证。

已实现（打包模板）：AMD64、ARM64 后端默认将 `WEKNORA_LANGUAGE` 设为空，采用前端发送的 `Accept-Language`（请求中的语言偏好）；未提供请求头时回退到 `zh-CN`。内置智能体名称、描述以及新入库任务生成摘要和问题时的语言提示跟随该偏好。显式配置非空 `WEKNORA_LANGUAGE` 仍优先于请求头。重新打包并升级、重建后端容器后生效；已有安装包和已生成内容不会自动改变。

## 其他 VOS App 以当前用户身份接入 HybRAG

HybRAG 同时保留三类认证方式：

- 个人 API Key：普通用户自己创建，外部系统用 `X-API-Key` 访问该用户允许的知识库。
- VOS/OAuth Bearer token：外部应用已经拿到普通用户的 VOS access token 时，直接用 `Authorization: Bearer <VOS token>` 调 HybRAG 业务 API；HybRAG 会校验并按该 VOS 用户映射出的 HybRAG 用户权限执行。
- VOS token exchange：其他 VOS app 也可以先把 VOS token 换成 HybRAG token，再用 HybRAG token 调用后续 API。

VOS 包默认同时提供两种 API 访问地址：VOS 应用路径 `https://<VOS地址>/app/com.ictrek.hybrag/api/v1`，以及宿主机直连 `http://<主机IP>:29081/api/v1`。前端直连地址为 `http://<主机IP>:29080`。如果安装时修改过 `HYBRAG_API_PORT` 或 `HYBRAG_FRONTEND_PORT`，以实际安装配置为准。

其他 VOS app 如果要“当前打开应用的 VOS 用户是谁，就访问 HybRAG 中对应用户的空间”，可以直接把 VOS/OAuth token 放到 HybRAG 业务 API 的 Bearer 头里，也可以先调用 token exchange 换取 HybRAG token。不要直接伪造 `X-External-User-ID`。`X-External-User-ID` 只适合可信服务端在已经持有 HybRAG 工作区级 API Key 的情况下做终端用户隔离；它不是免 key 用户登录机制。

### 身份映射规则

HybRAG 后端收到 VOS OIDC Fastpath 的应用 access token 后，会调用 `HYBRAG_VOS_OIDC_USERINFO_URL` 指向的 VOS `/v1000/oauth2/userinfo` 校验 token。老版本 VOS 没有 Fastpath 时，才使用 `HYBRAG_VOS_USERINFO_URL` 指向的 `/v1000/user/check` 降级校验。校验成功后按以下规则处理：

1. 读取 VOS 用户名。
2. 映射为 HybRAG 本地账户 `${username}@local`。
3. 如果账户不存在，自动创建账户。
4. 如果个人空间不存在，按 `WEKNORA_AUTH_DEFAULT_TENANT_MODE=create_personal` 自动创建 `${username}'s Workspace`。
5. 返回 HybRAG 自己签发的 `token` 和 `refresh_token`。
6. 后续 HybRAG API 调用使用 `Authorization: Bearer <HybRAG token>`。

如果调用方已经持有当前用户的 VOS/OAuth access token，也可以跳过 exchange，直接调用 HybRAG 业务 API：

```http
POST /api/v1/knowledge-chat/{session_id}
Authorization: Bearer <VOS/OAuth access token>
```

HybRAG 会按 HybRAG JWT、VOS OIDC `/v1000/oauth2/userinfo`、旧 VOS `/v1000/user/check` 的顺序识别 Bearer token。VOS token 识别成功后，请求主体就是该 VOS 用户映射出的 HybRAG 用户。

特殊规则：

- VOS `admin` 映射为 `admin@local`。
- `admin@local` 会自动提升为 HybRAG 系统管理员，并拥有跨空间管理能力。
- 普通 VOS 用户只拥有其 HybRAG 个人空间内的权限，除非后续由管理员把他加入其他空间或组织。

### 接口

同域 iframe VOS app 推荐优先使用 OIDC Fastpath，并把拿到的应用 access token 交给 HybRAG：

```http
POST /api/v1/auth/vos-oidc
Authorization: Bearer <VOS OIDC app access token>
```

老 VOS 版本或服务端只能拿到平台会话 token 时，可继续调用兼容入口：

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

`/api/v1/auth/vos-sso` 与 `/api/v1/auth/vos-token-exchange` 当前共用旧校验逻辑。新 VOS app 在 VOS 1.1+ 上应优先使用 `/api/v1/auth/vos-oidc`。

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

VOS 1.1+ 的推荐方式是声明 `manifest.yml` 的 `oauth2.client`，在同域 iframe 前端调用 `window.vos_platform.api.v1000.oauth2.authorize()` 与 `token()`；HybRAG app 已声明 `client_id=com.ictrek.hybrag`、`scope=openid profile email`、PKCE S256、`token_endpoint_auth_method=none`。只有当前 VOS 没有稳定 Fastpath 时，HybRAG 前端才使用临时兼容顺序。其他 app 如需在前端直接发起旧 exchange，可以按相同顺序读取 token：

1. 优先读取 `window.__VOS_APP_CONTEXT__.accessToken`。
2. 其次读取 `window.__VOS_APP_CONTEXT__.token`。
3. 再读取 `window.__VOS_ACCESS_TOKEN__`。
4. 如果这些注入值都不存在，再兼容读取 VOS 同源 `localStorage` 中以 `-core-access` 结尾的 store，例如 `core-access`、`VIVIBIT-core-access`。
5. 如果 store 使用 `secure-ls` 加密，则需要用 VOS 当前约定的加密 key 解出 access token。HybRAG 兼容环境变量 `VITE_VOS_STORE_SECURE_KEY`，未配置时使用当前默认值。

这套 localStorage/secure-ls 读取方式只是过渡方案。新的 VOS app 开发时，优先使用 OIDC Fastpath；只有兼容旧 VOS 时才读取平台会话 token 并调用旧 exchange。

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

新版本 app 镜像内置 `000079_upstream_wiki_and_tenant_api_key_repair` 兼容迁移，会补齐上游 Wiki 修订结构以及 `scope_type` 字段、`tenant_id` 可空约束、检查约束和索引。`000080` 到 `000082` 继续补偿上游与 ictrek 曾经占用同一迁移编号造成的结构缺口。以后遇到同类旧库，不要只手工改数据库；应先确认已经运行包含这些迁移的新 app 镜像。如果现场急需恢复页面，可以按迁移文件里的 SQL 临时补库，随后仍要升级到包含该迁移的镜像。

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
| Model Hub ReRank 总槽位 | `2` | 由 Model Hub rerank Ollama 提供；HybRAG 只把 ReRank 放进默认模型列表，不默认绑定知识库。 |

## 依赖和模型

V-Scheduler（统一模型网关与调度服务）可独立替代旧 Model Hub（本地模型管理服务），无需同时安装旧 Model Hub 或旧 V-Router（旧模型推理网关）。PGV（提供数据库和检索扩展的应用）是唯一必需依赖，最低版本在打包时从正式发布记录解析；安装清单见 [src/manifest.yml](src/manifest.yml)。`docker-compose.yml` 不启动本地模型或 PostgreSQL 服务。

PGV 文档中默认预置给 WeKnora/HybRAG 使用的连接信息是：

```text
DB_HOST=shared-pgv
DB_PORT=5432
DB_USER=weknora
DB_PASSWORD=weknora
DB_NAME=WeKnora
```

这里数据库名和用户仍使用 `weknora/WeKnora`，是为了兼容 PGV 默认初始化结果；VOS 应用显示名、容器名和 app id 改为 HybRAG 不要求改数据库名。安装 UI 会暴露 `WEKNORA_DB_HOST`、`WEKNORA_DB_PORT`、`WEKNORA_DB_USER`、`WEKNORA_DB_PASSWORD`、`WEKNORA_DB_NAME`，如果 PGV 安装时改过用户、密码或数据库名，在安装 HybRAG 时同步改这些值即可。

HybRAG 不启动自己的 Ollama（本地模型运行时）容器。仅使用 V-Scheduler 时，在其中准备模型并完成 HybRAG 应用绑定，然后在模型设置中选择已注册模型；推理统一经过 `http://v-scheduler:8080/v1`。打开 HybRAG 不请求旧 Model Hub 下载管理接口，旧下载浮窗仅在本页面显式触发旧下载或重试后启用。自动发现、旧部署兼容、鉴权及失败边界以[模型后端迁移](docs/model-backend-migration.md)为准。

VOS 安装包不放额外 `config/` 目录；App 容器入口脚本在运行时生成 `builtin_models.yaml`。升级会同步 `managed_by=yaml` 的模型行；管理员手动接管时需清空 `managed_by`。自动模式保留用户的云模型和默认选择。Embedding（将文本转换为检索向量）默认使用 1024 维 `bge-m3:latest`，已有知识库换模型需重新索引；ReRank（对检索结果重新评分和排序）不会自动绑定知识库，需由用户启用。

模型复制的权限、凭证处理及个人归属规则见 [用户指南](docs/USERGUIDE.md#共享模型与个人模型)。

默认 QA、VLM 使用 `thinking_control=reasoning_effort`，匹配网关后的 Ollama 模型；升级后启动应用，会自动更新由配置文件管理的默认模型设置。其他后端按实际接口选择参数。请求格式、失败重试和历史问题恢复步骤见 [思考参数兼容](docs/vos-ollama-prewarm.md#思考参数兼容当前源码已实现)。

Model Hub 下载、预热和原生 Ollama 接口检查见 [docs/vos-ollama-prewarm.md](docs/vos-ollama-prewarm.md)。用户界面操作见 [docs/USERGUIDE.md](docs/USERGUIDE.md)；新会话标题在回答结束后异步生成，输出上限、超时和兜底规则见其中的[快速问答](docs/USERGUIDE.md#11-快速问答)。

## 版本更新与 Release

`scripts/update_version.sh` 用于发布自增版本并触发 GitHub Actions。它不是 dry-run；执行成功后会修改版本文件、提交 commit、创建 `vos-hybrag-v${VERSION}` 触发 tag，并推送分支和 tag。飞书查表、pull 包打包、release notes 生成和 tar 上传由 `.github/workflows/vos-release.yml` 完成。

```bash
./scripts/update_version.sh patch
```

如果发布时需要校验 VOS changelog，先让 `$cl` 按目标版本归档并审核 `ictrek.app/CHANGELOG.md`，例如输入 `$cl 0.1.64`，然后执行；changelog 可以已提交，也可以留待脚本与版本号一起提交：

```bash
./scripts/update_version.sh patch --with-changelog
```

可选参数：

| 参数 | 行为 |
| --- | --- |
| `patch` | `0.0.1 -> 0.0.2`，默认值 |
| `minor` | `0.0.1 -> 0.1.0` |
| `major` | `0.0.1 -> 1.0.0` |
| `--with-changelog` | 校验目标版本标题和比较链接；有未提交修改时与 `VERSION` 一起提交，已提交时只提交 `VERSION`；除 changelog 外不允许有其他未提交改动 |

脚本会：

1. 自增 `ictrek.app/VERSION`。
2. 默认只提交 `VERSION`；使用 `--with-changelog` 时，同时提交 `ictrek.app/CHANGELOG.md` 的待提交修改，已提交的 changelog 无需再次修改。提交信息为 `chore: release VOS hybrag ${VERSION}`。
3. 创建并推送 `vos-hybrag-v${VERSION}` 触发 tag。
4. GitHub Actions 收到 tag 后执行 `.github/workflows/vos-release.yml`。

`--with-changelog` 不会调用或生成 changelog；它要求 `$cl` 已经写入下一版本标题 `## [${VERSION}] - YYYY-MM-DD`，并且工作区除 `ictrek.app/CHANGELOG.md` 外没有其他改动，也接受干净工作区。应用代码必须在执行发布脚本前单独提交。

已实现：目标版本必须有且只有一条 `[${VERSION}]: https://github.com/<owner>/<repo>/compare/<baseline-tag>...vos-hybrag-v${VERSION}` 引用定义（为版本标题指定跳转地址）。基线 tag（标记某次提交的版本标签）必须存在于本地且是当前提交的祖先；目标 tag 由本次发布创建。`$cl` 在归档时预先写入该链接，推送 tag 后才可在线访问。缺少标题、缺少或重复链接、链接格式或目标 tag 错误、基线缺失或不属于当前历史时，脚本会在修改 `VERSION`、提交和创建 tag 前退出。脚本只校验格式和本地基线，不检查网页可访问性；审计范围和仓库地址由 `$cl` 核实。

回归验证：在仓库根目录运行 `bash ictrek.app/scripts/test_update_version.sh`，使用临时仓库和本地远端，检查已提交、未暂存、已暂存 changelog 可发布，缺少目标版本、链接异常、基线缺失或不属于当前历史、存在其他改动时拒绝发布，以及默认模式仍要求工作区干净。

GitHub Actions 会：

1. 保留 `manifest.yml` 中固定且经过双端验证的最低兼容版本。
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

- 默认发布模式要求 HybRAG 工作区干净；`--with-changelog` 接受干净工作区或仅有待提交的 `ictrek.app/CHANGELOG.md`，其他改动仍会使脚本退出。
- `origin` 应指向发布目标仓库，例如 `git@github.com:ictrektech/WeKnora.git`。
- 本地只需要能向 HybRAG push 分支和 tag；不需要本地读取飞书，也不需要本地创建 GitHub Release。
- GitHub Actions 需要能读取飞书发布表，并能写 HybRAG release。

GitHub secrets：

| Secret | 用途 | 建议配置位置 |
| --- | --- | --- |
| `FEISHU_APP_ID` | 飞书应用 ID，用于读取镜像发布表 | Organization secret；HybRAG 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_APP_SECRET` | 飞书应用 secret | Organization secret；HybRAG 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_SPREADSHEET_TOKEN` | 可选；覆盖默认飞书表 token | Organization secret 或 repository secret |

HybRAG 是 public repo，因此当前组织级 Feishu secrets 可被 GitHub Actions 读取。其他没有私有依赖 release 的 VOS app 可继续沿用各自现有流程，不需要套用 HybRAG 的依赖 token 逻辑。

## 路由入口

`manifest.yml` 保留 `frontend.enabled: true` 和 `frontend.basePath: /app/com.ictrek.hybrag`，用于兼容当前仍从应用列表读取 `frontend_enabled/frontend_base_path` 的 VOS“我的应用”打开按钮。

`routers.yml` 使用顶层单 page 入口，避免在 VOS 侧边栏中再出现应用下的子页面。真实页面作为 VOS iframe 页面加载，并保留 `entry-point: true` 和 `embed: true`。Compose/Traefik 会把顶层文档请求 `/app/com.ictrek.hybrag/` 重定向到 VOS hash；iframe 请求继续进入真实应用页面，不会被重定向。

HybRAG 的固定入口契约是：

- `app id`: `com.ictrek.hybrag`
- `page.id`: `hybrag`
- `iframe-src`: `/app/com.ictrek.hybrag/?v=${VERSION}`
- VOS 内部侧边栏路径：`#/app/com.ictrek.hybrag/hybrag`

`scripts/package.sh` 会在生成 `app.tar.gz` 后校验以上字段；不匹配时直接失败。新增或修改入口时必须同步更新模板和脚本校验值。

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
- CI 飞书查表失败：检查 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_SPREADSHEET_TOKEN`，以及目标 profile 的 sheet 里是否存在 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 列，并且最新行有 tag。
- CI release 创建失败：查看 `VOS Pull Package Release` workflow 日志。若 package 已生成但 release 未创建，可在本地确认后补执行：

```bash
VERSION="$(cat VERSION)"
gh run view --repo ictrektech/WeKnora --log
gh release view "v${VERSION}" --repo ictrektech/WeKnora
```

### PGV BM25 索引升级与故障恢复

**已验证：**2026-10-14 核查的 AMD64 VOS 实例中，PGV `0.0.48` 与 HybRAG `0.1.67` 配合使用，实际运行 PostgreSQL `17.11`、`pg_search 0.25.11`（关键词检索扩展）和 `vector 0.8.4`（向量检索扩展）。应用账号连接、迁移版本 `140`（`dirty=false`）、已有数据的关键词与向量查询通过。同一 PGV 镜像的隔离库中，普通应用账号执行全部 141 个迁移文件及现有 Go 检索集成测试通过；写入、更新、删除、禁用过滤和索引查询通过。ARM VOS 的单文档升级验证见[顺序升级记录](#arm-vos-顺序升级验证)；持续大量导入及 VOS 升级前后逐表数据一致性仍**待验证**。

PGV `0.0.48` 的默认 ParadeDB 基础镜像为 `swr.cn-southwest-2.myhuaweicloud.com/ictrek/paradedb:amd64_0.25.11-pg17` 或 `arm64_0.25.11-pg17`；基础镜像不是完整 PGV 应用镜像。HybRAG 继续连接外部 `shared-pgv:5432`，不自行启动数据库，也不因本次验证提高 manifest 的最低依赖版本。

升级前备份数据库和角色、暂停导入和检索。PostgreSQL 16 → 17 是主版本迁移（数据库存储格式可能变化），不能只换镜像复用旧数据目录；由 PGV 管理员按 PGV 的迁移流程处理。已有数据库的扩展 SQL 定义也必须与镜像中的扩展库一致。仅换镜像不会更新 `pg_extension`（记录已安装扩展版本的系统表），应在使用扩展函数或恢复业务前确认升级完成。

应用迁移 `000117`、`000137` 遇到扩展升级权限不足时提示管理员处理，继续迁移；因此 `dirty=false` 本身不能证明扩展已升级。`000137` 仅在服务器提供目标包时尝试升级到 `pg_search 0.25.11`、`vector 0.8.4`，已执行的迁移不会因后来更换镜像再次运行。具体版本范围见 [ParadeDB 升级操作](../website-docs/01-getting-started/06-paradedb-upgrade.md#自动迁移的范围)。启动检查仍以 `pg_search 0.22.6` 为最低版本告警线，不用于判断扩展 SQL 定义与二进制是否匹配，也不会禁用向量检索。

遇到 `XX000` 的 CTID（数据库行的物理位置）或 heap（表数据页）访问错误时，先由管理员核对扩展与镜像版本。在维护窗口备份、暂停写入和检索后，以索引所有者或管理员身份连接 `WeKnora` 数据库，按需重建 BM25（关键词相关性评分）索引：

```sql
REINDEX INDEX public.embeddings_search_idx;
```

重建后验证关键词与向量检索，再恢复业务并重试失败文档。应用不会自动重建索引。当前 PGV `0.0.48` 容器不提供旧指引中的 `/init/pg-search-upgrade.sql`；不要按旧脚本路径恢复，也不要将扩展降到 `0.22.6`。正常升级不要求无条件重建全部索引；重建后仍失败时保留原始错误和备份，由管理员继续排查。

### ARM VOS 顺序升级验证

**已验证（用户实测反馈，记录于 2026-10-16）：**在 ARM 架构的 VOS `1.1.0-20261011.1.f0232367` 上，HybRAG `0.1.56` + PGV `0.0.33` 按先升级 PGV、再升级 HybRAG 的顺序，升级至 HybRAG `0.1.67` + PGV `0.0.49`。升级前及每一步升级后，从原文档已生成的问题发起对话，均返回正常回答。

| 步骤 | HybRAG | PGV | 操作与验收 | 结果 |
| --- | --- | --- | --- | --- |
| 升级前基线 | `0.1.56` | `0.0.33` | 向知识库上传一份文档，待解析并生成问题后，在聊天中选择生成的问题提问。 | 已验证：返回正常回答。 |
| 先升级 PGV | `0.1.56` | `0.0.49` | 仅升级 PGV，再从原文档已生成的问题提问。 | 已验证：返回正常回答。 |
| 再升级 HybRAG | `0.1.67` | `0.0.49` | 升级 HybRAG，再从原文档已生成的问题提问。 | 已验证：返回正常回答。 |

后续复测沿用上述顺序，每一步确认回答正常后再继续。证据来源为用户反馈，尚未核对运行日志、数据库及扩展实际版本。该结果覆盖本次 ARM 环境中的单文档问答升级；反向升级顺序、全新安装、全量数据一致性及其他 ARM 硬件仍**待验证**。

### Compose 数据库版本与升级边界

**已实现（配置）：**根目录 `docker-compose.yml` 与 `docker-compose.dev.yml` 的 `postgres`、`langfuse-db-init` 均使用 `paradedb/paradedb:v0.25.11-pg17`。健康检查通过 `127.0.0.1` 的 TCP（网络连接）探测，避免将仅开放本地 socket（进程间通信接口）的初始化临时服务器判为就绪。本地 ictrek 开发环境使用完整 PGV 镜像，见 [本地开发镜像替换](docs/local-dev/README.md#postgresql-镜像替换)。

新库直接使用新配置；已有 PostgreSQL 17 库须先备份并完成扩展 SQL 升级，见 [ParadeDB 升级操作](../website-docs/01-getting-started/06-paradedb-upgrade.md)。PostgreSQL 16 库需要主版本数据迁移。更新 Compose 不会自动迁移数据，也不会重新执行已完成的 `000137`。回滚须恢复升级前备份，不能只把镜像标签改回旧版。Helm 与历史部署模板未纳入本次版本更新；各自的旧配置不能当作当前 VOS 或本地开发配置使用。

## 模型后端迁移

HybRAG 同时兼容旧 Model Hub 与 V-Scheduler。安装参数、后台鉴权、完整模型 ID、调度责任和保留旧模式的方式见 [模型后端迁移](docs/model-backend-migration.md)。

### 从 V-Scheduler 添加模型 / Add models from V-Scheduler

在模型设置中打开“添加模型”，从 V-Scheduler 下拉框选择 Router 已注册且对 HybRAG 应用身份可见的模型，按所需用途选择聊天、视觉、Embedding 或 ReRank 类型后保存。列表每次打开都会重新加载，也可点击“刷新模型列表”。模型 ID 原样保留；网关地址自动填写，无需复制 API Key。服务端使用 V-Scheduler 为 HybRAG 下发的应用绑定凭证，并在凭证轮换后自动读取新值。绑定未完成时会提示加载失败，请先在 V-Scheduler 完成应用绑定。列表不根据模型名称猜测能力，实际支持的用途以网关调用结果为准。

Open Add Model in model settings, choose a registered Router model visible to the HybRAG app identity from the V-Scheduler dropdown, select its intended model type, and save. The list refreshes on open or with Refresh models. Model IDs are preserved and the gateway address is filled automatically. No API key needs to be copied: the server uses and reloads HybRAG's Scheduler app binding. Complete the app binding in V-Scheduler if discovery fails. Model capabilities are not guessed from names; use a model supporting the selected task.

当前应用绑定仅允许聊天（含视觉）、Embedding 和 ReRank；ASR 继续使用原有独立配置。个人私有上游不会因应用绑定而自动共享给 HybRAG。

The current app binding permits chat (including vision), embeddings and reranking. ASR keeps its existing separate configuration. Private personal upstreams are not automatically shared with the HybRAG app.

新安装推荐 `http://v-scheduler:8080`；`v-scheduler-gateway`、`v-router` 为固定兼容地址。后台私有身份支持前两个精确来源；`v-router` 旧记录仅允许重新绑定，避免与独立旧网关共存时发送凭据。通过能力协商迁移到正式地址，保留凭据及单调递增的 revision。显式配置和旧模型记录不自动改写。

### V-Scheduler cold reranking

Trusted V-Scheduler rerank endpoints use a 180-second default request deadline to
cover admission waiting and the first model load. Explicit configured timeouts and
caller cancellation remain authoritative; external endpoints retain their 60-second
default. No capacity or admission protection is bypassed.
