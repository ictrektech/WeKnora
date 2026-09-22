---
name: weknora-upstream-sync
description: Safely inspect, merge, and validate Tencent/WeKnora upstream/main into the ictrek WeKnora fork while preserving ictrek.app VOS packaging, branding, model defaults, compose persistence, and parent-repository submodule synchronization. Use when asked to sync, pull, merge, or resolve conflicts from Tencent/WeKnora, or to update the parent repository after an upstream merge.
---

# WeKnora 上游同步

## 目标与边界

将 Tencent/WeKnora 的 `upstream/main` 合并到 ictrek fork 的 `main`，保留上游功能更新和 ictrek 的本地部署、品牌及 VOS 决策。保留 merge 拓扑；不要把流程改成 rebase 或批量 cherry-pick。

先完整阅读仓库内的 [`ictrek.app/docs/upstream-sync.md`](../../../ictrek.app/docs/upstream-sync.md)，并遵守根目录 `AGENTS.md`。该文档是本项目上游同步流程的来源文件；本 Skill 只提炼执行时必须遵守的约束。

区分两类请求：

- 仅审计或解释时，只读检查 remote、分支、提交历史和文档，不改变 Git 配置、分支或工作区。
- 用户明确要求实际同步时，按下面流程执行；提交、推送和父仓库指针更新仍须保持在用户授权范围内。

禁止使用 `git reset --hard`、`git checkout --`、`git clean -fd` 或 force-push 来绕过冲突。不要覆盖用户已有改动，也不要将父仓库中的无关改动加入提交。

## 仓库拓扑

WeKnora 位于父仓库的 `apps/WeKnora` submodule 中。子仓库使用以下 remote：

~~~text
origin    git@github.com:ictrektech/WeKnora.git
upstream  git@github.com:Tencent/WeKnora.git
~~~

先确认实际位置和父仓库：

~~~bash
git rev-parse --show-toplevel
git rev-parse --show-superproject-working-tree
git status --short --branch
git remote -v
~~~

报告类任务发现缺少 `upstream` 时只报告。实际同步任务可以按用户授权添加或修正：

~~~bash
git remote add upstream git@github.com:Tencent/WeKnora.git
# 已存在但地址不正确时：
git remote set-url upstream git@github.com:Tencent/WeKnora.git
~~~

## 预检与差异审查

在 WeKnora 子仓库内执行：

~~~bash
git status --short --branch
git fetch upstream main --prune
git log --oneline --decorate --graph --max-count=30 --all
git diff --stat main..upstream/main
git diff --name-status main..upstream/main

# 找出 fork 与 upstream 自共同基线以来都修改过的文件
base=$(git merge-base main upstream/main)
comm -12 \
  <(git diff --name-only "$base"..main | sort) \
  <(git diff --name-only "$base"..upstream/main | sort)
~~~

合并前必须确认：

- 当前目标是 ictrek fork 的 `main`，而不是临时功能分支；如果用户指定了其他目标分支，明确记录该选择。
- 子仓库工作区没有未提交改动。发现改动时先停下并报告，不要借助 stash、reset 或覆盖操作替用户处理。
- `upstream/main` 已成功获取，且已经审查变更规模和重叠文件。
- 双方共同修改的文件已逐个做语义审查；无 Git 冲突也不能视为代码正确。
- 重点审查 `ictrek.app/*`、`build_image.sh`、`docker-compose.override.yml`、`docker/Dockerfile.frontend*`、`config/builtin_models.yaml`、`config/prompt_templates/*.yaml`、登录页和用户菜单等本地定制面。

## 合并策略

在确认预检通过后，从 WeKnora 子仓库的目标分支执行：

~~~bash
git checkout main
git merge --no-ff upstream/main
~~~

使用 `--no-ff` 保留上游同步的显式 merge commit。不要使用 rebase 或 cherry-pick 替代整合。合并无冲突时也继续执行后续验证。

## 冲突处理

先定位冲突，再逐文件判断，不要对整个仓库统一选择 ours 或 theirs：

~~~bash
git status --short
git diff --name-only --diff-filter=U
git diff --cc <conflicted-file>
~~~

遵循以下优先级：

1. 保留上游功能修复、测试和通用代码改进。
2. 如果上游版本覆盖了明确的 ictrek 部署或品牌决策，保留本地决策，并把上游通用逻辑有选择地合入。
3. 保持这些本地不变量：
   - `ictrek.app/` 继续承载当前 VOS app 的打包、发布、模型、构建和同步说明。
   - `ictrek.app/docs/legacy/` 仅作历史参考；不要用旧独立部署内容覆盖当前 VOS 行为。
   - `build_image.sh` 继续构建 ictrek 镜像并更新飞书发布表。
   - `config/builtin_models.yaml` 默认不写入某台部署机器的模型后端。
   - Prompt 模板保留 `Vivibit AI小助手` 身份；登录页和用户菜单保留 ictrek/Vivibit 链接。
   - 基础 `docker-compose.yml` 保留持久化行为，并确认 `SSRF_WHITELIST_EXTRA` 仍包含 `host.docker.internal`。

解决每个文件后单独暂存：

~~~bash
git add <resolved-file>
~~~

迁移编号是跨分支、跨部署环境的全局历史标识，不能只看文件名冲突是否被 Git 标记。迁移文件发生冲突时必须：

1. 检查 `migrations/versioned/` 和 `migrations/sqlite/` 中每个数字编号是否唯一，以及每个编号是否有匹配的 `.up.sql`/`.down.sql` 对。
2. 保留目标 fork 已经发布或可能已执行的迁移编号；不要重编号已有部署使用过的迁移。
3. 对后合入且占用已有编号的迁移分配目标 fork 中下一个未使用的编号，同时用 `git mv` 成对重命名 `.up.sql` 和 `.down.sql`。
4. 同步更新迁移文件内的镜像注释、文档链接、测试期望和版本说明；不能只改文件名。

当前 fork 的 `000097`–`000114` 已属于本地历史时，上游新增的 `000097`–`000102` 应顺延到 `000115`–`000120`，而不是重命名本地旧迁移。完成重编号后，在提交 merge 前必须运行本地门禁：

~~~bash
bash scripts/check-migration-files.sh migrations/versioned
bash scripts/check-migration-files.sh migrations/sqlite
git diff --check
~~~

`check-migration-files.sh` 会拒绝重复编号、缺失配对文件、`.up.sql`/`.down.sql` 名称不一致和非法文件名。任一命令失败都不能启动本地后端或提交 merge；先修复迁移集合，再继续验证。需要时再用空数据库执行 PostgreSQL 全量迁移，确认不仅文件名有效，SQL 也能从版本 0 升级完成。

完成冲突处理后先暂存并检查，验证通过前不要提交 merge：

~~~bash
git status --short
~~~

## 合并后验证

重新检查 ictrek 约束和工作区：

~~~bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \[\]" \
  config frontend/src/views/auth frontend/src/components/UserMenu.vue \
  docker-compose.yml docker-compose.override.yml ictrek.app
git diff --check
git status --short --branch
~~~

根据实际改动运行针对性验证：

- Go 代码：运行受影响包的 `go test`、必要的 `go vet`。
- 前端源码或路由受影响：必须运行 `npm run type-check`；运行时代码、构建配置或依赖受影响时，再运行 `npm run build-only`。
- 运行受影响的测试；全量测试仅在大范围同步或 CI 中执行。
- UI 或路由受影响：登录后从聊天页验证知识库、智能体、设置、新对话和其他会话之间的跳转，并确认控制台没有未处理的 `ReferenceError` 或 Vue 错误。
- `ictrek.app` 打包模板、镜像或迁移：按 `ictrek.app/docs/build-images.md` 和 VOS 文档选择相应验证。
- 不要因为上游同步自动重启远程部署或发布镜像；部署、构建和发布必须由用户另行授权。

`.github/workflows/vos-migration-check.yml` 只会在匹配路径的 `pull_request`，或推送到远端 `main` 后运行；本地 `git merge`、本地 `git commit` 和本地开发后端启动不会触发 GitHub Actions。该工作流会先运行 `scripts/check-migration-files.sh migrations/versioned`，再在空 PostgreSQL 数据库执行全量迁移，因此能拦截重复编号，但不能替代本地门禁，也不会检查 SQLite 迁移或当前本地开发数据库。未推送的本地合并必须把上面的检查命令视为必需步骤。

验证通过后提交 merge：

~~~bash
git status --short --branch
git commit
~~~

## 推送与父仓库指针

用户授权推送时，先推送 WeKnora 子仓库：

~~~bash
git status --short --branch
git push origin main
~~~

然后回到父仓库，只提交 WeKnora submodule 指针：

~~~bash
cd ../..
git status --short
git diff --submodule=log -- apps/WeKnora
git add apps/WeKnora
git commit -m "Update WeKnora upstream merge"
git push
~~~

父仓库有其他未提交改动时，不要使用 `git add -A`；只暂存 `apps/WeKnora`，并在报告中说明其他改动未处理。不要只留下本地 submodule 指针变化：完成的同步必须同时保留子仓库提交和父仓库指针提交。

## 完成报告

报告以下事实，不把计划写成已完成：

- 获取的 `upstream/main` 提交和合并目标分支。
- 是否产生 merge commit，以及实际解决的冲突文件和关键取舍。
- 执行过的验证命令及结果；未执行的验证说明原因。
- 子仓库和父仓库是否已提交、是否已推送。
- 剩余风险，例如未配置 `upstream`、远程推送未授权、迁移编号待复核或部署未验证。
