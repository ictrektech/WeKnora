# WeKnora VOS 应用打包说明

本目录包含 VOS app `com.ictrek.weknora` 的安装包模板。当前阶段只维护 pull 模式包骨架，后续调试稳定后再接入正式 CI 发布。

## 打包

正式发布入口是 `scripts/update_version.sh`。它只负责自增 `VERSION`、提交版本 commit、创建并推送 `vos-weknora-v${VERSION}` 触发 tag；GitHub Actions 收到 tag 后会读取飞书组件版本、生成 pull 包并发布 release。

本地 `package.sh` 只用于调试模板或手动验证。未设置 `PACKAGE_VERSION` 时读取当前 `ictrek.app/VERSION`，CI 会显式传入 tag 中解析出的 `PACKAGE_VERSION`。

```bash
cd apps/WeKnora/ictrek.app
./scripts/package.sh
```

脚本会生成一个 pull 模式安装包：

```text
dist/weknora_${VERSION}_pull.tar
```

安装包内只有 `app.tar.gz`，不会内置镜像归档。脚本会优先读取 `~/.feishu.components.json`，失败时回退到 `~/.feishu.json`，从飞书发布表读取 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 和 `ollama_server` 的最新镜像版本，并写入包内 `.env`。

打包脚本会校验 VOS 入口契约：`routers.yml` 必须声明 `entry-point: true` 和 `embed: true`，`docker-compose.yml` 必须把顶层文档请求 `/app/com.ictrek.weknora/` 重定向到 VOS 侧边栏内部路径。缺少这些字段时，VOS“我的应用”卡片的“打开”按钮可能只打开空白页或不能在侧边栏打开。

## Profiles

profile 按 `ollama_server` 的发布维度设置。WeKnora 自身 AMD 有无 CUDA 通用，ARM 有无 CUDA 通用，因此只查一个通用表；L4T 和 Thor Spark 单独查表。本应用只发布 4 个 profile。

| profile | 飞书 sheet | 说明 |
| --- | --- | --- |
| `AMD_with_cuda` | `AMD_with_cuda` | x86_64 / AMD 通用 WeKnora + Ollama |
| `ARM_with_cuda` | `ARM_with_cuda` | ARM 通用 WeKnora + Ollama |
| `l4t` | `l4t` | Jetson / L4T |
| `thor_spark` | `thor_spark` | Thor Spark |

安装时由 VOS 指定其中一个 profile。手动检查 compose 时也必须只启用一个 profile：

```bash
docker compose --profile AMD_with_cuda config
docker compose --profile l4t config
```

## 依赖和模型

`manifest.yml` 声明依赖 `com.ictrek.model-hub` 和 `com.ictrek.pgv`，但 `docker-compose.yml` 不启动 model_hub 或 Postgres 服务。WeKnora 包内只启动自身服务、Redis 和 `ollama_server` 容器；Postgres 通过 PGV 在 `vos_default` 网络上的 `shared-pgv:5432` 访问，后续可由 Model Hub 管理或预置 Ollama 模型目录。

初版 compose 使用两个 Ollama 容器：

- `weknora-ollama-qa-*`：聊天、图片理解/VLM。
- `weknora-ollama-embedding-*`：embedding。

WeKnora 默认通过 OpenAI-compatible gateway 访问：

- QA/VLM: `http://weknora-ollama-qa:11535/v1`
- Embedding: `http://weknora-ollama-embedding:11535/v1`

模型行仍建议通过 WeKnora UI 或后续配置文件显式添加；本包模板不在镜像中写死默认模型。

## 版本更新与 Release

`scripts/update_version.sh` 用于发布自增版本并触发 GitHub Actions。它不是 dry-run；执行成功后会修改版本文件、提交 commit、创建 `vos-weknora-v${VERSION}` 触发 tag，并推送分支和 tag。真正的依赖版本查询、飞书查表、pull 包打包、release notes 生成和 tar 上传由 `.github/workflows/vos-release.yml` 完成。

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
2. 提交 `VERSION`，提交信息为 `chore: release VOS weknora ${VERSION}`。
3. 创建并推送 `vos-weknora-v${VERSION}` 触发 tag。
4. GitHub Actions 收到 tag 后执行 `.github/workflows/vos-release.yml`。

GitHub Actions 会：

1. 使用 `VOS_DEPENDENCY_RELEASE_TOKEN` 查询 `model_hub_*_pull.tar` 与 `pgv_*_pull.tar` 的最新版本，并写入 CI 工作区内的 `manifest.yml`。
2. 使用 `FEISHU_APP_ID`、`FEISHU_APP_SECRET` 和可选 `FEISHU_SPREADSHEET_TOKEN` 写出 `~/.feishu.components.json`。
3. 调用 `scripts/package.sh`，从飞书发布表读取 WeKnora 四镜像和 `ollama_server` 的最新版本，生成 `dist/weknora_${VERSION}_pull.tar`。
4. 生成 release notes。
5. 创建公开 release tag `v${VERSION}`，并上传 pull 模式 tar 包。`vos-weknora-v${VERSION}` 只用于触发 CI，不作为公开 release tag。

执行前检查：

```bash
cd apps/WeKnora
git status --short
git remote get-url origin
git fetch --tags origin
```

要求：

- WeKnora 工作区必须干净；脚本会在存在未提交改动时退出。
- `origin` 应指向发布目标仓库，例如 `git@github.com:ictrektech/WeKnora.git`。
- 本地只需要能向 WeKnora push 分支和 tag；不需要本地读取飞书，也不需要本地创建 GitHub Release。
- GitHub Actions 需要能读取依赖 release、读取飞书发布表，并能写 WeKnora release。

GitHub secrets：

| Secret | 用途 | 建议配置位置 |
| --- | --- | --- |
| `VOS_DEPENDENCY_RELEASE_TOKEN` | 读取同组织私有依赖仓库 release assets，例如 `model_hub`、`pgv` | Organization secret，`Repository access` 可选 `All repositories`，权限 `Contents: Read-only` |
| `FEISHU_APP_ID` | 飞书应用 ID，用于读取镜像发布表 | Organization secret；WeKnora 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_APP_SECRET` | 飞书应用 secret | Organization secret；WeKnora 是 public repo，可使用当前组织 public repositories 范围 |
| `FEISHU_SPREADSHEET_TOKEN` | 可选；覆盖默认飞书表 token | Organization secret 或 repository secret |

WeKnora 是 public repo，因此当前组织级 Feishu secrets 可被 GitHub Actions 读取。其他没有私有依赖 release 的 VOS app 可继续沿用各自现有流程，不需要套用 WeKnora 的依赖 token 逻辑。

## 路由入口

`routers.yml` 使用固定的 group/page 入口。真实页面作为 VOS iframe 页面加载，并保留 `entry-point: true` 和 `embed: true`。为兼容仍读取 `frontend_base_path` 的旧“打开”按钮，Compose/Traefik 会把顶层文档请求 `/app/com.ictrek.weknora/` 重定向到 VOS hash；iframe 请求继续进入真实应用页面，不会被重定向。

WeKnora 的固定入口契约是：

- `app id`: `com.ictrek.weknora`
- `group.id`: `com-ictrek-weknora`
- `page.id`: `weknora`
- `iframe-src`: `/app/com.ictrek.weknora/?v=${VERSION}`
- VOS 内部侧边栏路径：`#/app/com.ictrek.weknora/com-ictrek-weknora/weknora`

`scripts/package.sh` 会在生成 `app.tar.gz` 后校验以上字段；不匹配时直接失败。新增或修改入口时必须同步更新模板和脚本校验值。

当前这条说明里的“其他 VOS app”包括 `model_hub`、`pgv`、`motrix-next`、`cc_setup`。这些 app 暂不因为 WeKnora 的私有依赖查询需求改变发布流程。

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
git tag --list "vos-weknora-v${VERSION}" "v${VERSION}" --format='%(refname:short) %(objectname:short)'
```

如果脚本失败，按阶段处理：

- 本地脚本失败：通常是工作区不干净、版本号非法、触发 tag 或公开 release tag 已存在。先用 `git status --short`、`git tag --list 'vos-weknora-v*' 'v*'` 检查。
- CI 依赖 release 查询失败：检查 `VOS_DEPENDENCY_RELEASE_TOKEN` 是否可用，是否有同组织仓库 `Contents: Read-only` 权限。
- CI 飞书查表失败：检查 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`FEISHU_SPREADSHEET_TOKEN`，以及目标 profile 的 sheet 里是否存在 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox`、`ollama_server` 列，并且最新行有 tag。
- CI release 创建失败：查看 `VOS Pull Package Release` workflow 日志。若 package 已生成但 release 未创建，可在本地确认后补执行：

```bash
VERSION="$(cat VERSION)"
gh run view --repo ictrektech/WeKnora --log
gh release view "v${VERSION}" --repo ictrektech/WeKnora
```

## GitHub Actions 依赖查询验证

本机 `gh` 能查到私有仓库不代表 GitHub Actions 默认 `GITHUB_TOKEN` 也能查到。`Check VOS Dependency Release Access` workflow 用于验证 CI 能否读取 VOS 依赖仓库 release assets。

WeKnora 仓库需要配置名为 `VOS_DEPENDENCY_RELEASE_TOKEN` 的 repository 或 organization secret。推荐使用 fine-grained PAT：

- `Repository access`: `All repositories`
- `Permissions`: 只添加 `Contents: Read-only`

配置后手动运行 `Check VOS Dependency Release Access`。日志中应出现类似：

```text
Using VOS_DEPENDENCY_RELEASE_TOKEN for dependency release lookup.
ictrektech/model_hub: latest visible pull asset version is 0.0.13
ictrektech/pgv: latest visible pull asset version is 0.0.13
```

CI 发布流程应复用同一个 secret 作为 `GH_TOKEN`，不要依赖当前仓库默认 `GITHUB_TOKEN` 读取其他私有仓库。
