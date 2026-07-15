# WeKnora VOS 应用打包说明

本目录包含 VOS app `com.ictrek.weknora` 的安装包模板。当前阶段只维护 pull 模式包骨架，后续调试稳定后再接入正式 CI 发布。

## 打包

```bash
cd apps/WeKnora/ictrek.app
./scripts/package.sh
```

脚本会生成一个 pull 模式安装包：

```text
dist/weknora_${VERSION}_pull.tar
```

安装包内只有 `app.tar.gz`，不会内置镜像归档。脚本会优先读取 `~/.feishu.components.json`，失败时回退到 `~/.feishu.json`，从飞书发布表读取 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 和 `ollama_server` 的最新镜像版本，并写入包内 `.env`。

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

`scripts/update_version.sh` 用于发布自增版本。它不是 dry-run；执行成功后会修改版本文件、提交 commit、推送 tag，并在 GitHub Releases 上传 pull 包。

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
2. 从当前仓库 `origin` 推断 GitHub 组织，例如 `git@github.com:ictrektech/WeKnora.git` 会推断为 `ictrektech`。
3. 通过 GitHub CLI 查询同组织下 `ictrektech/model_hub` 和 `ictrektech/pgv` 的 release assets，找到最新 `model_hub_*_pull.tar` 与 `pgv_*_pull.tar`，并写入 `manifest.yml` 的依赖版本。
4. 调用 `scripts/package.sh`，从飞书发布表读取 WeKnora 四镜像和 `ollama_server` 的最新版本，生成 `dist/weknora_${VERSION}_pull.tar`。
5. 提交 `VERSION` 和 `manifest.yml`，提交信息为 `chore: release VOS weknora ${VERSION}`。
6. 推送当前分支，并推送触发 tag `vos-weknora-v${VERSION}`。
7. 使用 `gh release create v${VERSION}` 创建公开 release，或在 release 已存在时使用 `gh release upload --clobber` 覆盖上传 tar 包。

执行前检查：

```bash
cd apps/WeKnora
git status --short
git remote get-url origin
gh auth status
gh api 'repos/ictrektech/model_hub/releases?per_page=100' --jq '.[0].tag_name'
gh api 'repos/ictrektech/pgv/releases?per_page=100' --jq '.[0].tag_name'
ls -l ~/.feishu.components.json ~/.feishu.json 2>/dev/null
```

要求：

- WeKnora 工作区必须干净；脚本会在存在未提交改动时退出。
- `origin` 应指向发布目标仓库，例如 `git@github.com:ictrektech/WeKnora.git`。如果不是这个仓库，可以用 `WEKNORA_RELEASE_REPO=ictrektech/WeKnora` 显式覆盖。
- `gh` 需要能读取依赖仓库 release，并能在 WeKnora 创建 GitHub Release。
- 本机发布时，`gh auth status` 至少应包含 `repo`；如果 GitHub 因 workflow 文件改动拒绝 release/tag 相关操作，再执行一次 `gh auth refresh -h github.com -s workflow`。这个授权会保存到本机 GitHub CLI 登录态，不是每次发布都要做。
- 需要飞书凭据 `~/.feishu.components.json` 或 `~/.feishu.json`，用于读取组件镜像 tag。

发布命令：

```bash
cd apps/WeKnora/ictrek.app
./scripts/update_version.sh patch
```

发布后验证：

```bash
VERSION="$(cat VERSION)"
gh release view "v${VERSION}" --repo ictrektech/WeKnora \
  --json tagName,targetCommitish,url,assets
git tag --list "vos-weknora-v${VERSION}" "v${VERSION}" --format='%(refname:short) %(objectname:short)'
tar tf "dist/weknora_${VERSION}_pull.tar"
```

如果脚本失败，按阶段处理：

- 依赖 release 查询失败：确认 `gh api 'repos/ictrektech/model_hub/releases?per_page=100'` 和 `gh api 'repos/ictrektech/pgv/releases?per_page=100'` 可读；如果在 CI 里失败，检查 `VOS_DEPENDENCY_RELEASE_TOKEN`。
- 飞书查表失败：确认目标 profile 的 sheet 里存在 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox`、`ollama_server` 列，并且最新行有 tag。
- 包已生成但 release 创建失败：先用 `git status --short`、`git tag --list 'vos-weknora-v*' 'v*'`、`gh release view v${VERSION}` 判断已经完成到哪一步。若 commit 和 tag 已推送但 release 未创建，可补执行：

```bash
VERSION="$(cat VERSION)"
gh release create "v${VERSION}" "dist/weknora_${VERSION}_pull.tar" \
  --repo ictrektech/WeKnora \
  --target "$(git rev-parse HEAD)" \
  --title "v${VERSION}" \
  --notes "WeKnora VOS pull package ${VERSION}."
```

如果 release 已存在但资产缺失或需要覆盖：

```bash
VERSION="$(cat VERSION)"
gh release upload "v${VERSION}" "dist/weknora_${VERSION}_pull.tar" \
  --repo ictrektech/WeKnora \
  --clobber
```

如依赖 release 不在默认同组织 repo，可用环境变量覆盖：

```bash
WEKNORA_MODEL_HUB_RELEASE_REPO=ictrektech/model_hub \
WEKNORA_PGV_RELEASE_REPO=ictrektech/pgv \
./scripts/update_version.sh patch
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
