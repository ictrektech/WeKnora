# 上游同步

本文面向 ictrek fork 维护者，是 Tencent/WeKnora 上游同步流程的唯一事实源。先固定两端提交，审查本地兼容行为及测试，再合并、验证和提交。下方附英文流程。

| 项目 | 当前要求 |
| --- | --- |
| 支持 | 人工审查本地定制、兼容契约（已有配置必须保持的行为）及测试迁移 |
| 必需 | 删除或重构定制实现时，逐项确认行为去向并运行对应回归测试 |
| 未实现 | 自动发现全部 fork 定制或阻断兼容回归的专用 CI 门禁 |
| 下一步 | 按下文固定提交，完成本地兼容保留检查后再合并 |

## remote 设置

在 WeKnora submodule 中保留两个 remote：

```bash
git remote -v
```

期望：

```text
origin    git@github.com:ictrektech/WeKnora.git
upstream  git@github.com:Tencent/WeKnora.git
```

如果缺少 upstream：

```bash
git remote add upstream git@github.com:Tencent/WeKnora.git
```

## 合并流程

```bash
cd apps/WeKnora
git status --short
git fetch upstream main --prune
git checkout main
fork_before=$(git rev-parse HEAD)
upstream_target=$(git rev-parse upstream/main)
```

无冲突的 merge 也必须做语义检查。先列出 fork 与 upstream 自共同基线以来都修改过的文件，逐个审查：

```bash
base=$(git merge-base "$fork_before" "$upstream_target")
comm -12 \
  <(git diff --name-only "$base" "$fork_before" | sort) \
  <(git diff --name-only "$base" "$upstream_target" | sort)
```

冲突处理原则：

- 保留当前 VOS app 文档和配置：`ictrek.app/`；
- 保留本地品牌和链接定制；
- 保留空的 `config/builtin_models.yaml` 默认行为，不把某台机器的模型后端写进镜像；
- 保留 compose 中持久化配置，并确认基础 `docker-compose.yml` 的 `SSRF_WHITELIST_EXTRA` 仍包含 `host.docker.internal`；
- 上游功能代码尽量合入，不做无关重构。

## 本地兼容保留检查

重叠文件列表只是审查线索。上游可能删除旧模块和测试，或把逻辑迁到另一个文件；必须同时查看两侧自共同基线以来的改动，不能只检查合并冲突或固定目录。

```bash
git diff --name-status --find-renames "$base" "$fork_before"
git diff --name-status --find-renames "$base" "$upstream_target"
```

对上游删除、重命名或重构的区域，读取 fork 原有实现、调用方、配置和测试。合并后逐项记录“保留”“已迁移（新实现和测试位置）”或“明确废弃（迁移说明）”。

删除定制测试前，必须找到覆盖相同输入和输出的替代测试；否则先补齐。缺少覆盖或尚未验证时，不得报告同步完成。

模型适配变化时，至少核对以下 ictrek 扩展：

| 已有配置 | 必须保留的请求行为 | 验证范围 |
| --- | --- | --- |
| `extra_config.thinking_control=think` | 显式开/关发送顶层 `think:true/false`；未指定时不发送 | 从模型配置解析到最终请求 JSON；流式和非流式 |
| `extra_config.thinking_control=reasoning_effort` | 旧布尔开/关发送顶层 `reasoning_effort:medium/none`；未指定时不发送 | 从模型配置解析到最终请求 JSON；流式和非流式 |

还需确认其他已支持值和未知非空值的原有回退行为未改变。表中是必须保留的契约，不代表某次合并或已部署镜像已验证通过。

历史案例：`f735f55c0` 删除旧思考适配及测试时遗漏了上述两项兼容，因此必须检查行为和测试的迁移去向。

审查完成后，合并已固定的上游提交：

```bash
git merge --no-ff --no-commit "$upstream_target"
```

`--no-ff` 保留 merge 拓扑，`--no-commit` 将提交留到验证通过后。

## 合并后检查

冲突处理并暂存后，先与保存的合并前提交比较，复核删除和迁移结果：

```bash
git diff --cached --name-status --find-renames "$fork_before"
```

未提交的合并不能用 `HEAD^1` 代替 `fork_before`；此时 `HEAD` 仍指向合并前提交。

重点查这些本地定制是否还在：

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  frontend config ictrek.app docker-compose.yml docker-compose.override.yml
```

前端源码或路由发生变化时，提交前必须完成类型检查；运行时代码、构建配置或依赖发生变化时，再执行构建。测试按受影响范围运行，全量测试交给大范围同步或 CI：

```bash
cd frontend
npm run type-check
cd ..
```

UI 或路由发生变化时，再登录验证聊天页到知识库、智能体、设置、新对话及其他会话的跳转，并确认控制台没有未处理的 `ReferenceError` 或 Vue 错误；运行时代码、构建配置或依赖发生变化时执行 `npm run build-only`。

模型适配变化时，迁移并运行上述请求契约的回归测试；当前相关包为：

```bash
go test ./internal/models/runtime ./internal/models/api/openaicompletions ./internal/models/chat
```

包路径变化时调整命令，保留契约覆盖并记录实际测试及结果。部署验证须在授权范围内检查正文非空、问题落库和推荐接口结果，不能只看 `HTTP 200` 或任务 `success`；未执行则记为待验证。

验证通过后再看状态并提交：

```bash
git status --short
git diff --check
git commit
```

如需构建镜像，按 [build-images.md](build-images.md) 走构建和飞书更新流程。部署和发布以 [../README.md](../README.md) 的 VOS app 流程为准；旧独立部署文档只在 [legacy](legacy/) 中备查。

## 提交顺序

先提交并推送 WeKnora submodule：

```bash
git add <changed-files>
git commit -m "..."
git push origin main
```

然后回到总仓库更新 submodule 指针：

```bash
cd ../..
git add apps/WeKnora
git commit -m "Update WeKnora"
git push origin main
```

---

# Upstream Sync

This note records how to pull functional updates from the upstream WeKnora
repository into the ictrek fork while preserving ictrek-specific changes.

## Sources

```text
ictrek fork:      git@github.com:ictrektech/WeKnora.git
upstream source:  git@github.com:Tencent/WeKnora.git
```

Use `origin` for the ictrek fork and `upstream` for the Tencent source.

## Preflight

Run these commands inside the WeKnora submodule:

```bash
cd apps/WeKnora
git status --short --branch
git remote -v
```

If `upstream` is missing, add it:

```bash
git remote add upstream git@github.com:Tencent/WeKnora.git
```

If it already exists, make sure it points to the expected source:

```bash
git remote set-url upstream git@github.com:Tencent/WeKnora.git
```

Fetch the latest upstream branch:

```bash
git fetch upstream main --prune
```

Before merging, inspect what upstream changed:

```bash
git log --oneline --decorate --graph --max-count=30 --all
git diff --stat main..upstream/main
git diff --name-status main..upstream/main
```

Even a conflict-free merge needs a semantic review. List files changed by both
sides since their common base and review them one by one:

```bash
fork_before=$(git rev-parse main)
upstream_target=$(git rev-parse upstream/main)
base=$(git merge-base "$fork_before" "$upstream_target")
comm -12 \
  <(git diff --name-only "$base" "$fork_before" | sort) \
  <(git diff --name-only "$base" "$upstream_target" | sort)
```

Follow the [local compatibility preservation checklist](#本地兼容保留检查)
before merging; it defines the protected contracts and replacement-test requirements.

Pay special attention to files that overlap with ictrek changes:

```text
AGENTS.md
build_image.sh
docker-compose.override.yml
docker/Dockerfile.frontend
docker/Dockerfile.frontend.dockerignore
config/builtin_models.yaml
config/prompt_templates/*.yaml
frontend/src/views/auth/Login.vue
frontend/src/components/UserMenu.vue
ictrek.app/*
```

## Merge

Merge upstream into the ictrek fork branch:

```bash
git checkout main
git merge --no-ff --no-commit "$upstream_target"
```

Keep the merge topology and defer the commit until verification passes.

## Conflict Handling

For conflicts, keep upstream functional fixes unless they overwrite deliberate
ictrek deployment or branding decisions.

Preserve these ictrek decisions unless the operator explicitly changes them:

- `ictrek.app/` keeps the current VOS app package, release, model, build, and sync notes.
- `ictrek.app/docs/legacy/` is reference-only. Do not prefer legacy standalone deployment content over current VOS app behavior.
- `build_image.sh` remains the ictrek image build and Feishu update entrypoint.
- `config/builtin_models.yaml` ships no deployment-specific model rows by
  default.
- prompt templates identify the assistant as `Vivibit AI小助手`.
- login and user menu links point to ictrek/Vivibit destinations.
- local persistence behavior stays documented and stable. The base
  `docker-compose.yml` must keep `host.docker.internal` in
  `SSRF_WHITELIST_EXTRA` so model rows that call host-mapped vLLM/Ollama
  backends do not fail when a deployment intentionally omits
  `docker-compose.override.yml`.

Useful conflict commands:

```bash
git status --short
git diff --name-only --diff-filter=U
git diff --cc <conflicted-file>
```

After resolving each conflict:

```bash
git add <resolved-file>
```

Migration numbers are global history across branches and deployments. A Git
merge can succeed while the migration source is still invalid. Before
committing the merge:

```bash
bash scripts/check-migration-files.sh migrations/versioned
bash scripts/check-migration-files.sh migrations/sqlite
git diff --check
```

Keep migration numbers already published by the fork. If an incoming upstream
migration uses one of those numbers, assign it the next unused number and
rename its `.up.sql` and `.down.sql` files together with `git mv`. Update
comments, documentation links, tests, and version references as well. For
example, if the fork already owns `000097`–`000114`, incoming upstream
`000097`–`000102` must become `000115`–`000120`; never rename the existing
fork migrations.

The checker rejects duplicate numbers, missing pairs, mismatched `.up.sql` /
`.down.sql` names, and malformed filenames. Do not start the local backend or
commit the merge while either checker fails. When practical, also execute the
PostgreSQL migrations from an empty database.

After resolving conflicts, stage and inspect the result. Do not commit before
the verification gates pass:

```bash
git status --short
```

## Verification

Compare the staged merge with `fork_before` and complete the
[post-merge checks](#合并后检查) before committing.

After the merge, re-check the ictrek invariants:

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  config frontend/src/views/auth frontend/src/components/UserMenu.vue \
  docker-compose.yml docker-compose.override.yml ictrek.app
```

For code-level verification, use the build path documented in
`build-images.md`. Build and deployment should run on the selected remote host,
not locally, unless the task explicitly asks for a local check.

If frontend source or routing changed, run before committing:

```bash
cd frontend
npm run type-check
cd ..
```

Run `npm run build-only` when runtime code, build configuration, or dependencies
changed. Run affected tests; reserve the full suite for broad syncs or CI. For
UI or routing changes, log in and smoke-test navigation from chat to knowledge
bases, agents, settings, new chat, and another session. The browser console
must have no unhandled `ReferenceError` or Vue error.

The VOS migration workflow runs only for matching `pull_request` events or
after a push to remote `main`; a local merge or local commit does not trigger
GitHub Actions. It runs the versioned migration checker and a full PostgreSQL
empty-database migration, but it does not validate SQLite migrations or the
current local development database. Always run the local checker commands
before starting a local backend and before committing the merge.

After verification passes, commit the merge:

```bash
git status --short --branch
git diff --check
git commit
```

## Push And Parent Repo Update

Push the submodule first:

```bash
git status --short --branch
git push origin main
```

Then update the parent repository's submodule pointer:

```bash
cd ../..
git status --short
git add apps/WeKnora
git commit -m "Update WeKnora upstream merge"
git push
```

Do not leave a completed upstream sync only as a local submodule change; the
submodule commit and the parent repository pointer should both be pushed.
