# VOS HybRAG 正式发布流程

**当前状态：**正式发布仍走 `update_version.sh` 和自动发布流程。本地打包预检还需要可读取 PGV（提供数据库和检索扩展的应用）发布记录的 GitHub 凭证；执行前先满足第 0 节条件，再使用第 4 节兼容新旧 `gh` 的命令。

## 定位

本操作清单（Runbook）面向开发者和 Agent（自动执行任务的助手），记录从代码变更到 VOS 安装包、GitHub Release 和 VOS App Store 发布的线性流程。它不是 Skill（按需调用的任务说明），不会被自动触发，也不负责安装或升级正在运行的 VOS 应用。

当前仓库的正式入口是 `ictrek.app/scripts/update_version.sh`。该脚本创建并推送 `vos-hybrag-vX.Y.Z` 触发 tag（标记提交的版本标签）；GitHub Actions（自动执行发布任务的服务）随后负责打包、生成 GitHub Release Notes（发布说明）、创建公开 `vX.Y.Z` tag、上传 pull 包（安装时拉取镜像的包）并发布到 VOS App Store（应用商店）。

| 产物 | 规则 |
| --- | --- |
| VOS 应用版本 | `ictrek.app/VERSION` |
| VOS CI（自动检查和打包流程）触发 tag | `vos-hybrag-vX.Y.Z` |
| GitHub 公开 tag | `vX.Y.Z` |
| VOS pull 包 | `ictrek.app/dist/hybrag_X.Y.Z_pull.tar` |
| 自有镜像 tag | `amd_X.Y.Z`、`arm_X.Y.Z` |

GitHub Release Notes 由 `.github/workflows/vos-release.yml` 根据提交历史生成，不会自动读取 `ictrek.app/CHANGELOG.md`。`$cl` 只负责维护 CHANGELOG，需要记录时单独调用；`update_version.sh --with-changelog` 只负责将已审核的 CHANGELOG 与 `VERSION` 合并到 release commit。

## 事实源

执行前后以以下文件为准，不要在本 Runbook 中复制或改写脚本内部逻辑：

- [`ictrek.app/README.md`](../../ictrek.app/README.md)：发布说明和验证命令。
- [`ictrek.app/scripts/update_version.sh`](../../ictrek.app/scripts/update_version.sh)：版本递增、提交、触发 tag 和推送。
- [`ictrek.app/scripts/package.sh`](../../ictrek.app/scripts/package.sh)：VOS pull 包生成和包内容校验。
- [`resolve_dependencies.py`](../../ictrek.app/scripts/resolve_dependencies.py) 和 [`dependency-releases.json`](../../ictrek.app/scripts/dependency-releases.json)：依赖版本查询和仓库对应关系。
- [`appstore_tunnel.sh`](../../ictrek.app/scripts/appstore_tunnel.sh)：CI 查询应用商店依赖目录的 SSH（加密远程连接）隧道。
- [`build_image.sh`](../../build_image.sh)：AMD/ARM 镜像构建、推送和飞书发布表更新。
- [`ictrek.app/docs/build-images.md`](../../ictrek.app/docs/build-images.md)：镜像构建机、凭证和运行目录边界。
- [`.github/workflows/vos-release.yml`](../../.github/workflows/vos-release.yml)：CI 打包、Release Notes、GitHub Release 和 App Store 发布。
- [`$cl`](../skills/cl/SKILL.md)：只维护 `ictrek.app/CHANGELOG.md`，不提交、不打 tag、不发布 Release。

## 线性顺序

```text
代码变更 -> 测试检查 -> 提交代码
  -> AMD/ARM 镜像和飞书版本表 -> 打包预检
  -> [可选] $cl 归档 CHANGELOG
  -> update_version.sh（可选地一次提交 CHANGELOG + VERSION）
  -> vos-hybrag-vX.Y.Z -> CI 打包和 Release Notes
  -> vX.Y.Z GitHub Release -> VOS App Store -> 发布后校验
```

## 0. 发布前置条件

- 明确本次 SemVer（主版本号、次版本号、修订号组成的版本格式）递增类型：`patch`、`minor` 或 `major`。
- 当前分支是预期发布分支，`origin` 指向发布仓库。
- AMD 和 ARM 构建主机可用，并配置构建用 `~/.feishu.json`；不要打印、提交或复制凭据。
- 本地打包预检优先使用只读组件凭证 `~/.feishu.components.json`；脚本会在不可用时尝试 `~/.feishu.json`。不要把只读组件凭证传给 `build_image.sh`。
- 执行本地打包预检时，`gh` 须已登录能读取 `ictrektech/pgv` 发布记录的账号。预检命令读取现有凭证后传给 `VOS_DEPENDENCY_RELEASE_TOKEN`，不要打印凭证；未登录时先执行 `gh auth login`。
- GitHub Actions 已配置 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`APPSTORE_IP` 和 `APPSTORE_TOKEN`。
- 默认模式下 `update_version.sh` 会拒绝 dirty worktree；使用 `--with-changelog` 时只允许 `ictrek.app/CHANGELOG.md` 未提交。本地 tag 引用过期时，由操作者明确执行 `git fetch --tags origin`。

## 1. 检查代码和测试

从仓库根目录执行：

```bash
cd /home/czy/p/vai/apps/WeKnora
git status --short --branch
git diff --check
make test
```

检查测试结果、VOS 模板、入口契约和镜像配置。不要把测试失败、未完成功能或未验证的性能结论写入 CHANGELOG。

## 2. 确定发布版本

先计算本次发布版本，供后续镜像构建、打包预检和可选 CHANGELOG 归档使用：

```bash
RELEASE_PART=patch # 本次使用 minor/major 时，同步修改第 5 节命令
CURRENT_VOS_VERSION="$(tr -d '[:space:]' < ictrek.app/VERSION)"
IFS=. read -r VOS_MAJOR VOS_MINOR VOS_PATCH <<< "$CURRENT_VOS_VERSION"
case "$RELEASE_PART" in
  patch) VOS_PATCH=$((VOS_PATCH + 1)) ;;
  minor) VOS_MINOR=$((VOS_MINOR + 1)); VOS_PATCH=0 ;;
  major) VOS_MAJOR=$((VOS_MAJOR + 1)); VOS_MINOR=0; VOS_PATCH=0 ;;
  *) echo "invalid RELEASE_PART: $RELEASE_PART" >&2; exit 1 ;;
esac
RELEASE_VERSION="${VOS_MAJOR}.${VOS_MINOR}.${VOS_PATCH}"
printf "%s\n" "$RELEASE_VERSION"
```

应用代码必须在进入第 3 节前提交。此时不要求存在 CHANGELOG；如果后续调用 `$cl`，它会在第 4 节打包预检通过后再更新 `ictrek.app/CHANGELOG.md`。

## 3. 构建并推送镜像

构建时必须使用第 2 节计算出的 `RELEASE_VERSION`。`patch` 发布可以省略 `--tag`，因为 `build_image.sh` 默认生成下一个 patch 镜像 tag；`minor` 或 `major` 发布必须显式传入带平台前缀的 tag。

在原生 AMD 构建机执行（只更新 VOS 包使用的 `AMD_with_cuda`）：

```bash
# AMD 构建机
./build_image.sh --target amd --sheet AMD_with_cuda
# minor/major 发布改用：
# ./build_image.sh --target amd --sheet AMD_with_cuda --tag "amd_${RELEASE_VERSION}"
```

在原生 ARM 构建机执行（只更新 VOS 包使用的 `ARM_with_cuda`）：

```bash
# ARM 构建机
./build_image.sh --target arm --sheet ARM_with_cuda
# minor/major 发布改用：
# ./build_image.sh --target arm --sheet ARM_with_cuda --tag "arm_${RELEASE_VERSION}"
```

未传 `--tag` 时，脚本会根据当前 `ictrek.app/VERSION` 计算下一个 patch 版本，因此两台构建机应在同一版本基线执行。每条命令会构建并推送四个自有镜像，并只将对应 tag 写入指定的飞书 sheet；日期行统一使用 `YYYYMMDD`（例如 `20260911`）。

确认 `AMD_with_cuda` 和 `ARM_with_cuda` 两个 sheet 的 `weknora`、`weknora-ui`、`weknora-docreader`、`weknora-sandbox` 都已写入对应新 tag 后，再进入第 4、5 节。远程构建时，先按 [`build-images.md`](../../ictrek.app/docs/build-images.md) 同步源码；构建同步目录只用于构建，不要在那里运行应用 compose。

如果构建或飞书写表失败，停在本阶段修复；不要继续创建 VOS 触发 tag。

## 4. 本地打包预检（可选）

本地预检检查模板、镜像版本读取、安装清单（Manifest）、容器配置（Compose）、包结构和 PGV 发布记录，不代替 CI 正式打包或 VOS 安装验证。

**当前行为：**未设置 `VOS_DEPENDENCY_CATALOG_URL` 时，脚本通过 GitHub 查询 PGV 最新正式发布及其 `_pull.tar` 资产，并将包内最低依赖写为 `>=查询到的版本`；它不会自动使用 `gh` 保存的登录凭证。CI 则通过应用商店隧道查询版本并检查包是否可下载。发布记录检查不能证明运行兼容性。

从仓库根目录的 Bash（命令解释器）执行以下整段。它先尝试新版 `gh auth token`，不支持时回退到旧版 `gh config get oauth_token`；取凭证或打包失败时停止预检：

```bash
(
  set -e
  : "${RELEASE_VERSION:?请先执行第 2 节计算发布版本}"
  if ! vos_dependency_token="$(gh auth token --hostname github.com 2>/dev/null)"; then
    vos_dependency_token="$(gh config get oauth_token --host github.com)"
  fi
  if [[ -z "$vos_dependency_token" || "$vos_dependency_token" == *[[:space:]]* ]]; then
    printf '读取到无效凭证，请执行 gh auth login 后重试。\n' >&2
    exit 1
  fi
  VOS_DEPENDENCY_RELEASE_TOKEN="$vos_dependency_token" \
  FEISHU_CONFIG_FILE="${HOME}/.feishu.components.json" \
  PACKAGE_VERSION="$RELEASE_VERSION" \
  ./ictrek.app/scripts/package.sh
  test -f "ictrek.app/dist/hybrag_${RELEASE_VERSION}_pull.tar"
  tar tf "ictrek.app/dist/hybrag_${RELEASE_VERSION}_pull.tar"
)
```

预期外层包只有 `app.tar.gz`；内层不应残留 `__APP_VERSION__`、未替换的镜像变量或额外的 `config/` 目录。`${VOS_APP_STORAGE_PATH}` 等安装时提供的变量必须保留，不能将所有 `${...}` 都当成错误。预检失败时不要执行下一步。

| 输出 | 含义与处理 |
| --- | --- |
| `Dependency resolution failed: HTTP Error 404` | 先检查本地凭证是否传入，以及账号是否能读取 PGV 仓库和正式发布；不要仅凭 `gh` 已登录就判断脚本已获得凭证。 |
| `Invalid header value` | 检查取凭证命令是否失败、误将报错文字作为凭证；使用上面的兼容命令，不要输出凭证排查。 |
| `VOS_APP_STORAGE_PATH ... is not set` | 本地 Compose 检查缺少 VOS 安装时提供的数据目录；若打包成功且包内变量仍保留，不代表成品路径已被写成空值。 |
| `command substitution: ignored null byte in input` | 当前包校验将 `icon.png` 等二进制文件一并读入文本变量触发；影响临时校验文本，不会改写包内图标。 |

**已验证（2026-10-22，本地）：**`gh 2.4.0` 可通过旧版取法读取凭证并查询 PGV `0.0.49`，用户提供的 HybRAG `0.1.75` 打包日志以 `Done` 结束；检查成品确认数据目录变量保留、图标与源文件一致。新版取凭证和失败分支仅做模拟检查，VOS 全新安装和升级仍待验证。

## 5. 可选更新 CHANGELOG 并创建 VOS 触发 tag

CHANGELOG 不是发布前置条件。开发过程中可直接调用 `$cl` 或 `$cl unreleased`，它们会以最近一个可达的 `vos-hybrag-v*` 基线审计到当前 `HEAD`，并将缺失条目补充到 `Unreleased`。发布时，在打包预检通过后调用 `$cl ${RELEASE_VERSION}`，将现有 `Unreleased` 和本次审计结果归档为 `## [${RELEASE_VERSION}] - YYYY-MM-DD`；归档不会创建新的空 `Unreleased`。

检查结果，但不要单独提交 CHANGELOG：

```bash
git diff --check
git diff -- ictrek.app/CHANGELOG.md
```

如果 `$cl` 成功生成了 CHANGELOG，确认应用代码已经提交，工作区只剩 `ictrek.app/CHANGELOG.md` 后执行：

```bash
# patch 示例；minor/major 与第 2 节的 RELEASE_PART 保持一致
cd ictrek.app
./scripts/update_version.sh patch --with-changelog
```

如果跳过 `$cl` 或它没有产出可用 CHANGELOG，则要求工作区完全干净，并执行不带 `--with-changelog` 的旧命令：

```bash
cd ictrek.app
./scripts/update_version.sh patch
```

需要其他递增类型时，将两个示例中的 `patch` 都替换为 `minor` 或 `major`。脚本会更新 `ictrek.app/VERSION`，创建 `chore: release VOS hybrag ${VERSION}` 版本提交，创建并推送 `vos-hybrag-v${VERSION}`，再推送当前分支。

`--with-changelog` 会校验 CHANGELOG 含有下一版本标题 `## [${VERSION}] - YYYY-MM-DD`，拒绝除 `ictrek.app/CHANGELOG.md` 外的任何未提交改动，并在同一个 commit 中提交 `ictrek.app/CHANGELOG.md` 和 `ictrek.app/VERSION`。如果本次不维护 CHANGELOG，且工作区完全干净，才使用不带该参数的旧命令。

该脚本有外部写操作，不要手工提前创建同名 tag，也不要使用强制推送覆盖远程 tag。若远程已存在触发 tag 或公开 tag，停止并先检查现有发布状态。

## 6. 观察 GitHub Actions

```bash
cd /home/czy/p/vai/apps/WeKnora
gh run list --repo ictrektech/WeKnora --workflow vos-release.yml --limit 5
RUN_ID=123456789 # 替换为上一步对应的实际运行编号
gh run watch "$RUN_ID" --repo ictrektech/WeKnora --exit-status
```

workflow 会检查数据库迁移和模型后端，生成组件凭证并建立依赖目录隧道。打包步骤从 `vos-hybrag-v*` 解析应用版本，通过 `PACKAGE_VERSION` 传给 `package.sh`；脚本读取飞书镜像版本，通过应用商店解析 PGV 依赖后打包。随后关闭查询隧道、生成 Release Notes、创建 `v${VERSION}` GitHub Release 并上传 pull 包，最后另建上传隧道发布同一个包到 VOS App Store。

Release Notes 来自提交历史。GitHub Release 创建在 App Store 发布之前，因此两者不是原子操作；Release 成功但 App Store 失败时，不要重复创建 tag 或盲目重跑整个流程。

## 7. 发布后校验

```bash
cd /home/czy/p/vai/apps/WeKnora
RELEASE_VERSION="$(tr -d '[:space:]' < ictrek.app/VERSION)"
gh release view "v${RELEASE_VERSION}" --repo ictrektech/WeKnora --json tagName,targetCommitish,url,assets,body
git ls-remote --tags origin "refs/tags/vos-hybrag-v${RELEASE_VERSION}" "refs/tags/v${RELEASE_VERSION}"
gh run view "$RUN_ID" --repo ictrektech/WeKnora --log | rg "hybrag_|Published|release|package"
```

确认：

- `vos-hybrag-v${RELEASE_VERSION}` 和 `v${RELEASE_VERSION}` 都存在；
- GitHub Release 资产包含 `hybrag_${RELEASE_VERSION}_pull.tar`；
- Release Notes、包名和版本一致；
- CI 日志包含 VOS App Store 发布成功信息；
- 本次流程没有执行 VOS 安装、升级、重启或数据卷删除。

## 异常处理边界

- 工作区不干净：默认模式直接停止；`--with-changelog` 只允许 `ictrek.app/CHANGELOG.md`，否则停止并清理或提交明确属于本次发布的改动，不覆盖用户改动。
- `$cl` 失败：如果没有留下 CHANGELOG 改动，不影响发布，可走默认模式；如果留下了半成品改动，先完成或明确处理该文件，再选择 `--with-changelog` 或恢复到干净工作区。
- 镜像构建、飞书写表或打包预检失败：修复后重新验证，不创建触发 tag。
- `update_version.sh` 报 tag 已存在：停止，检查现有 CI 和 Release，不强制覆盖远程 tag。
- CI 打包失败：保留触发 tag，查看失败步骤和日志，不重新递增无关版本。
- GitHub Release 成功但 App Store 失败：保留 GitHub Release，单独处理 App Store 发布，不执行安装或升级。

完成后报告 VOS 版本、两个 tag、GitHub Release URL、包名、CI run ID 和 App Store 发布结果；不要报告 secret、token、内部主机地址或客户数据。
