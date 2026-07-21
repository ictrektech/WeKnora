# 上游同步

本文件记录 ictrek fork 从 Tencent/WeKnora 上游拉取并合并功能更新的流程。中文说明在上方，英文原文在下方。

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
git fetch upstream
git checkout main
git merge upstream/main
```

冲突处理原则：

- 保留当前 VOS app 文档和配置：`ictrek.app/`；
- 保留本地品牌和链接定制；
- 保留空的 `config/builtin_models.yaml` 默认行为，不把某台机器的模型后端写进镜像；
- 保留 compose 中持久化配置，并确认基础 `docker-compose.yml` 的 `SSRF_WHITELIST_EXTRA` 仍包含 `host.docker.internal`；
- 上游功能代码尽量合入，不做无关重构。

## 合并后检查

重点查这些本地定制是否还在：

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  frontend config ictrek.app docker-compose.yml docker-compose.override.yml
```

再看状态：

```bash
git status --short
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
git merge --no-ff upstream/main
```

If there are no conflicts, continue to the verification step.

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

Complete the merge:

```bash
git commit
```

## Verification

After the merge, re-check the ictrek invariants:

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  config frontend/src/views/auth frontend/src/components/UserMenu.vue \
  docker-compose.yml docker-compose.override.yml ictrek.app
```

For code-level verification, use the build path documented in
`build-images.md`. Build and deployment should run on the selected remote host,
not locally, unless the task explicitly asks for a local check.

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
