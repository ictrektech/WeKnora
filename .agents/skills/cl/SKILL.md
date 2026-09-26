---
name: cl
description: 审计 VOS HybRAG 应用相关 Git 提交并维护项目级 CHANGELOG.md，整理面向应用用户、开发者和部署人员的可验证变更。默认适配当前仓库的 ictrek.app/CHANGELOG.md；无参数调用默认维护 Unreleased，按最近 VOS release tag 到 HEAD 补充缺失条目，指定 SemVer 时归档到对应版本；不生成或抽取 GitHub Release Notes。用户调用 $cl，或要求检查遗漏变更、更新 CHANGELOG、汇总指定范围的 VOS HybRAG 差异、补录历史版本时使用。
---

# VOS HybRAG Changelog

维护目标项目中 VOS HybRAG 应用的 CHANGELOG.md，把提交和实际 diff 整理成面向应用用户、开发者和部署人员的产品变化。本技能只负责记录 CHANGELOG，不负责生成、抽取、发布或同步 GitHub Release Notes。

无参数调用 `$cl` 即维护 `## [Unreleased]`：以目标 `HEAD` 可达的最近 VOS release tag 为基线，审计 `tag..HEAD` 的已提交变化，读取现有 `Unreleased`，只补充缺失条目并去重，不记录上次执行的 commit。指定 SemVer 时才执行版本归档；归档会将当前 `Unreleased` 与本次审计结果合并到目标版本，不创建新的空 `Unreleased`。

## 快速调用

按以下轻量语法解析 `$cl` 后的文本，不要求用户构造完整审计 prompt：

| 调用 | 行为 |
| --- | --- |
| `$cl` | 维护 `## [Unreleased]` |
| `$cl unreleased` | 显式维护 `## [Unreleased]`，与 `$cl` 相同 |
| `$cl 0.1.59` | 将 `Unreleased` 和当前审计结果归档到 `## [0.1.59] - YYYY-MM-DD` |

上述三种调用都使用默认基线和默认目标：基线为目标 `HEAD` 可达的最近 `TAG_GLOB`，目标为 `HEAD`；只有版本归档使用当前日期生成版本标题。不增加其他版本、日期或范围参数；输入不符合这三种形式时，先报告支持的调用方式，不修改 CHANGELOG。版本文件不存在或指定版本值无效时，同样不修改 CHANGELOG。

## 解析项目配置

先从用户指令、仓库文档、版本脚本、VOS Manifest 和 CI workflow 中确定以下配置。不要因为目录名或提交标题相似就猜测配置。

按以下优先级处理：

1. 用户明确指定的 VOS 应用目录、CHANGELOG 路径、版本文件、版本 tag 模式或审计范围优先。
2. 当前仓库存在 `ictrek.app/VERSION`、`ictrek.app/scripts/update_version.sh` 和 VOS 发布 workflow 时，使用下表作为本仓库默认值。
3. 其他 VOS HybRAG 项目中，查找包含 VOS Manifest、打包脚本或发布 workflow 的应用目录；从版本脚本和 workflow 读取实际 tag 前缀，不要默认使用本仓库的名称。
4. 无法唯一确定目标文件或版本线时，先报告候选项和证据，不修改任何 CHANGELOG。

当前仓库默认值：

| 配置 | 默认值 | 证据或用途 |
| --- | --- | --- |
| VOS HybRAG 应用目录 | `ictrek.app/` | 当前项目的 VOS 打包、安装、运行和文档入口 |
| CHANGELOG | `ictrek.app/CHANGELOG.md` | 本技能唯一的默认写入目标 |
| VOS 版本文件 | `ictrek.app/VERSION` | VOS 应用版本线 |
| VOS 触发 tag | `vos-hybrag-v[0-9]*` | `.github/workflows/vos-release.yml` 和 `update_version.sh` |
| 打包脚本 | `ictrek.app/scripts/package.sh` | 生成 VOS pull 安装包 |
| 版本脚本 | `ictrek.app/scripts/update_version.sh` | 更新版本并创建 VOS 触发 tag |

`vos-hybrag-v*` 是当前仓库的实际 tag 规则，不代表其他项目的通用命名；当前规则中不使用 `vos-weknora-v*`。将本技能用于其他项目时，只有在其仓库证据确认相同规则后才沿用该模式。

## 固定边界

- 从仓库根目录工作，先读取适用的 `AGENTS.md`。
- 只修改解析出的 CHANGELOG 文件。当前仓库默认只修改 `ictrek.app/CHANGELOG.md`，不要修改项目根目录 `CHANGELOG.md`、`docs/ictrek/CHANGELOG.md`、`cli/CHANGELOG.md`、`mcp-server/CHANGELOG.md` 或其他组件日志。
- 将 VOS 专属的打包、安装、升级、运行、模型、部署和用户文档变化记录在解析出的 VOS 应用目录下。当前仓库的 VOS 专属内容放在 `ictrek.app/`，`docs/ictrek/` 只保留兼容跳转。
- 使用解析出的 VOS 版本文件解释 VOS 版本；若仓库根目录还有 `VERSION`，只作为上下文记录，不与 VOS 版本线混用。
- 不自动执行 `git fetch` 或刷新远程引用。可以读取本地已有的分支和 tag；如果基线过期或缺失，报告出来并说明需要用户刷新。
- 不自动创建、移动或删除 tag，不提交、不推送、不创建或修改 GitHub Release。即使用户要求“准备发布”，本技能也只更新目标 CHANGELOG。
- 默认只审计已提交历史；保留用户现有未提交改动，不把工作区改动当作已实现变化。只有用户明确要求时，才单独检查并标注未提交内容。
- 不泄露凭据、内部账号、私有主机地址、客户数据、知识库标识或未公开测试数据。

## 选择工作模式

采用以下一种模式：

1. **未发布记录（默认）**：无参数或使用 `unreleased` 时，以最近可达的 VOS release tag 到 `HEAD` 为审计范围，读取并更新 `## [Unreleased]`。保留已有内容，只补充缺失条目并去重；重复执行必须保持结果稳定，不记录上次执行的 commit。
2. **版本归档（显式）**：收到 SemVer 参数时，将同一审计范围和现有 `Unreleased` 条目合并到 `## [X.Y.Z] - YYYY-MM-DD`，不创建新的空 `## [Unreleased]`。这只是 CHANGELOG 整理，不代表创建了 GitHub Release。
3. **历史补录**：仅在用户明确给出已有 VOS 版本、tag 或 commit 时，审计该范围；不要把目标之后的提交写入该历史版本。

如果操作会改写已有版本、范围或历史条目，且用户没有明确目标，先询问用户。

## 确定审计范围

### 版本线与基线

1. 默认目标为 `HEAD`；用户指定 tag 或 commit 时使用指定目标。
2. 使用已解析的变量 `APP_DIR`、`CHANGELOG`、`VERSION_FILE` 和 `TAG_GLOB` 进行检查。例如当前仓库对应：

   ```bash
   APP_DIR="ictrek.app"
   CHANGELOG="ictrek.app/CHANGELOG.md"
   VERSION_FILE="ictrek.app/VERSION"
   TAG_GLOB="vos-hybrag-v[0-9]*"
   ```

3. 先读取并记录版本、现有日志状态和工作区状态：

   ```bash
   tr -d '[:space:]' < "$VERSION_FILE"
   if test -f VERSION && test "$VERSION_FILE" != VERSION; then tr -d '[:space:]' < VERSION; fi
   test -f "$CHANGELOG" && git log -1 --format='%H %cI %s' -- "$CHANGELOG" || true
   git status --short --branch
   ```

   只用 `VERSION_FILE` 解释 VOS 版本；根目录 `VERSION` 只作为上下文记录。两者不一致可能是正常的版本线差异，不要自行合并。
4. 列出目标可达的 VOS 触发 tag：

   ```bash
   git tag --merged <target> --sort=-version:refname --list "$TAG_GLOB"
   ```

5. 按以下优先级确定基线：

   - 用户明确给出的起点或范围；
   - 与 `VERSION_FILE` 或用户指定 VOS 版本对应的可达触发 tag；
   - 没有精确匹配时，使用目标可达的最近触发 tag，并在报告中说明版本文件与 tag 的差异；
   - 没有任何 VOS tag 时，审计目标历史中涉及 `APP_DIR` 或明确影响 VOS 运行时的提交，并报告“无 VOS tag 基线”。

6. 不用根项目 `vX.Y.Z`、GitHub Release 发布时间或其他组件版本作为 VOS CHANGELOG 的隐含基线。公开 `vX.Y.Z` tag 只有在用户明确指定，或能与同版本 VOS 触发 tag 对应时才作为辅助证据。

无参数或 `unreleased` 模式不记录、读取或推断上次 `$cl` 执行的 commit；每次都重新使用目标可达的最近 VOS release tag 作为基线，审计完整的 `tag..HEAD`，再与现有 `Unreleased` 条目合并去重。

### 提交审计

1. 保留与本任务无关的工作区改动：

   ```bash
   git status --short --branch
   ```

2. 有基线时，按时间顺序列出范围内的非合并提交：

   ```bash
   git log --reverse --no-merges --format='%H%x09%cI%x09%s' <base>..<target>
   ```

   没有基线时，列出目标历史中涉及 VOS 应用目录的提交，并额外检查会改变 VOS 镜像运行时、默认模型或安装契约的根目录提交。
3. 对每个候选提交检查实际内容，不要只凭提交标题决定：

   ```bash
   git show --stat --summary <hash>
   git show --name-status --format=fuller <hash>
   git show --format=fuller <hash> -- <relevant-paths>
   ```

4. 不因提交来自上游就自动跳过。只要它改变了 VOS 包内行为、镜像运行时、安装/升级契约、模型配置或应用文档，就纳入；与 VOS 无关的通用项目变化不纳入本日志。
5. 将同一用户结果的连续提交合并为一个 CHANGELOG 条目，不逐条复制 commit message。提交数量和 SHA 只在执行报告中用于溯源，正文不做 commit dump。

### 版本比较与提交溯源

- CHANGELOG 正文保持面向用户，不在每个条目后堆叠 Commit SHA。执行报告提供“新增或调整的 CHANGELOG 条目 → 关键 Commit”的紧凑映射；一个条目由多个提交共同形成时，列出主要提交并说明已聚合。
- 仓库存在稳定、可浏览且不含凭据的代码托管地址时，在 CHANGELOG 末尾维护版本标题使用的 Markdown 引用链接。优先使用项目确认的 canonical remote；可以从 SSH remote（例如 `git@github.com:org/repo.git`）转换为对应的 HTTPS 网页地址，但不得把用户名、令牌或其他凭据写入文档。
- GitHub 仓库使用以下格式；其他托管平台只有在仓库证据确认其 Compare URL 规则后才添加，不要猜测：

  ```markdown
  [Unreleased]: https://github.com/org/repo/compare/<baseline-tag>...HEAD
  [0.1.63]: https://github.com/org/repo/compare/<previous-tag>...<version-tag>
  ```

- `Unreleased` 链接始终使用本次审计基线到 `HEAD`。已有版本链接使用该版本实际审计的起止 tag；历史跳号时沿用用户明确给出的范围，不虚构缺失 tag。
- 只有起止引用都已存在并且范围可验证时才添加版本链接。SemVer 归档时若目标 tag 尚不存在，保留条目但暂不添加最终版本链接，并在报告中说明；目标 tag 创建后，下次维护时补齐。
- 更新引用定义时去重并保留无关链接；引用定义放在文件末尾，不改变现有版本正文顺序。

## 筛选内容

纳入以下变化：

- VOS app Manifest、router、compose profile、configs、入口路径、应用品牌和安装 UI 契约。
- 应用目录中的打包脚本、版本脚本、打包校验、包内文件、镜像引用和版本注入行为。
- VOS 应用的安装、升级、持久化目录、依赖服务、Docker Compose、镜像构建和运行排错变化。
- Gateway、QA/VLM/embedding/rerank 默认模型、预热、并发、健康检查和相关配置变化；仅在目标项目实际存在这些组件时纳入。
- 会改变 VOS 用户结果、工作流、API、权限、兼容性、错误恢复或数据边界的后端、前端和依赖变化。
- 安全、隐私、凭据、SSRF、数据保留、删除、审计和权限边界变化。
- 能支撑 VOS 使用、安装、升级或验证的自动化测试、样例和应用文档。
- 影响 VOS 稳定性、可观测性或资源使用且有代码、测试或运行证据的内部改动。

当前仓库还应按实际 diff 补查以下 VOS 相关证据；其他项目只检查其对应文件：

- `ictrek.app/scripts/package.sh`
- `ictrek.app/scripts/update_version.sh`
- `ictrek.app/src/`
- `build_image.sh`
- `.github/workflows/vos-release.yml`

跳过以下变化：

- 纯 merge、纯同步、版本号或 tag housekeeping，除非提交同时包含需要记录的 VOS 产品变化。
- CHANGELOG 自身修改、纯格式化、纯重命名、无行为变化的内部重构和单独生成文件更新。
- 只增加测试但没有改变 VOS 产品行为或重要兼容性边界的提交。
- 与 VOS 包、运行时、安装契约和应用文档均无关的通用项目变化。
- 尚未实现的路线图、猜测性承诺和未经验证的准确率、节省时间、容量或稳定性结论。

## 使用稳定格式

创建或维护解析出的 CHANGELOG 时，使用中文，保留精确的产品、API、配置、镜像和路径标识。新内容采用以下章节顺序，仅保留有内容的三级章节；已有历史章节不为统一格式而重写：

```markdown
# VOS HybRAG Changelog

记录 VOS HybRAG 应用的可验证变化。

## [Unreleased]

### 重点变化

- **变化名称** — 面向用户或部署人员的结果和适用范围。

### 新增

- **变化名称** — 新增能力、入口或配置。

### 优化

- **变化名称** — 行为改进及用户或运维影响。

### 修复

- **变化名称** — 修正的问题、受影响场景和验证范围。

### 打包与部署

- **变化名称** — 安装包、镜像、compose、迁移、配置或升级变化。

### 模型与运行

- **变化名称** — 模型、Gateway、预热、健康检查、并发或运行时变化。

### 安全与权限

- **变化名称** — 具体的安全、隐私或权限边界变化。

### 文档与验证

- **变化名称** — 用户文档、操作指南、样例、测试或验证范围。

### 移除与不兼容

- **变化名称** — 被移除或不兼容的能力、配置、接口及迁移动作。

### 已知限制

- **变化名称** — 已确认的当前限制及其影响；不要写路线图承诺。
```

上例是默认的 `Unreleased` 格式。使用 `$cl X.Y.Z` 归档时，将目标范围和现有 `Unreleased` 条目合并到 `## [X.Y.Z] - YYYY-MM-DD`；归档完成后不为了占位添加空的 `Unreleased`。

版本标题不带 `v`，版本号取自用户指定的 SemVer 值，例如 `## [0.1.0] - 2026-09-03`。发布归档必须使用精确的 `## [X.Y.Z] - YYYY-MM-DD` 标题；默认和 `unreleased` 模式使用 `## [Unreleased]`。触发 tag 使用仓库实际的 `TAG_GLOB` 规则；当前仓库为 `vos-hybrag-vX.Y.Z`。不要改写已有版本标题。

## 编写规则

- 使用中文，先写用户或部署人员得到的结果，再写必要技术细节。
- 每个条目只表达一个可验证变化；将相关提交聚合，避免复制提交列表。
- 保留精确的 API、环境变量、迁移、镜像、profile、路径、版本和配置标识；首次出现的专门术语用短语解释。
- 只有存在代码、测试、文档或运行证据时，才使用“支持、完成、修复、兼容”等确定表述；不要把目标设计写成当前行为。
- 明确记录不兼容变更、升级动作、默认值、数据迁移、权限影响和已知限制。
- 不使用“革命性、完整、全自动”等宣传语，不夸大性能、安全或质量结论。
- 优先链接 VOS 应用目录下当前维护的公开文档；当前仓库优先链接 `ictrek.app/` 下文档。不要在正文写入内部主机、凭据、客户信息或未公开数据。
- 与已有 `Unreleased` 或历史版本逐项去重；历史版本只读，除非用户明确要求修订指定版本。

## 更新文件

1. 在编辑前按“快速调用”确定工作模式、目标版本和审计范围。默认或 `unreleased` 模式不需要版本号，使用最近可达的 VOS release tag 到 `HEAD`；SemVer 模式使用指定版本和当前日期。版本文件无效、版本参数无效或调用形式不支持时，不修改文件并报告问题。
2. 完整读取解析出的 CHANGELOG；默认或 `unreleased` 模式下文件不存在时，创建标题、说明和 `## [Unreleased]`；SemVer 模式下文件不存在时，创建标题、说明和目标 `## [X.Y.Z] - YYYY-MM-DD`。
3. 默认或 `unreleased` 模式下，逐项比较 `tag..HEAD` 的候选变化与现有 `Unreleased`：保留已有条目，只补充缺失条目并去重；不清空、替换、静默改写或无必要地重排已有内容。没有缺失变化时保持文件不变，重复执行结果必须稳定。
4. SemVer 归档模式下，将当前 `Unreleased` 中能由本次审计范围证明属于目标版本的条目，与候选变化合并到目标版本章节；目标章节不存在时，在说明段之后、其他历史版本之前插入。未能证明属于目标版本的人工草稿保留原处并在报告中说明；迁移后若 `Unreleased` 为空，删除这个空标题，不创建新的空 `Unreleased`。
5. 按“版本比较与提交溯源”维护文件末尾的引用定义；无法验证可浏览仓库地址或 Compare URL 时不添加链接，并在报告中说明。
6. 所有模式都保留已有历史版本章节，不把 CHANGELOG 最近一次修改 commit 当作审计基线，不记录或推断上次 `$cl` 执行位置；所有条目都要逐项去重，不改写历史版本的原有措辞。
7. 使用补丁方式编辑，检查最终 diff 和格式：

   ```bash
   git diff --check
   git diff -- "$CHANGELOG"
   ```

8. 当前仓库确认没有改动项目根目录 `CHANGELOG.md`、`docs/ictrek/CHANGELOG.md`、组件日志、历史版本或用户已有的无关工作区改动；其他项目按其适用的仓库边界检查。
9. 本技能完成后不执行 fetch、tag、commit、push 或 GitHub Release 操作。

## 汇报结果

完成后报告：

- 解析出的 VOS 应用目录、CHANGELOG、版本文件和 tag 规则；说明采用的是用户指定值、当前项目默认值还是其他项目的仓库证据。
- 审计目标、VOS 基线来源、起止 tag/commit，以及无法确定基线时采用的回退策略。
- VOS 版本文件、根目录 `VERSION`（如存在）和目标 CHANGELOG 顶部版本；说明它们的版本线差异。
- 纳入的 VOS 产品变化、聚合后的条目数和对应提交数量。
- 新增或调整的 CHANGELOG 条目到关键 Commit SHA 的紧凑映射；CHANGELOG 正文不重复这些 SHA。
- 跳过的 housekeeping、无 VOS 影响变化、通用项目变化和不确定内容。
- 写入或修改的 CHANGELOG 章节和文件路径。
- 新增、更新或无法生成的版本 Compare 链接及其原因。
- 采用的工作模式、目标版本（如有）和审计基线；说明是补充 `Unreleased` 还是执行版本归档，以及是否因无缺失变化而保持文件不变。
- 无法验证、需要用户判断或后续补充证据的内容。
- 明确说明本次只维护目标 CHANGELOG，没有生成或发布 GitHub Release Notes。
