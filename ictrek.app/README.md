# HybRAG VOS 应用打包说明

本目录包含 VOS app `com.ictrek.hybrag` 的安装包模板。当前只发布 pull 模式安装包：本地 `update_version.sh` 只创建触发 tag，GitHub Actions 负责读取飞书和依赖 release、打包并发布正式 release。

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

`manifest.yml` 声明依赖 `com.ictrek.model-hub >=0.0.29` 和 `com.ictrek.pgv`，但 `docker-compose.yml` 不启动 model_hub 或 Postgres 服务。`0.0.29` 起 Model Hub 提供独立的 QA 与 embedding Ollama 预热运行时。HybRAG 包内只启动自身服务、Redis 和 Neo4j；Postgres 通过 PGV 在 `vos_default` 网络上的 `shared-pgv:5432` 访问，模型调用通过 Model Hub 暴露的两个 gateway。

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
