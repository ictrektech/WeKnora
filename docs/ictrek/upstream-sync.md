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
docs/ictrek/*
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

- `docs/ictrek/` keeps local deployment, build, model, and sync notes.
- `build_image.sh` remains the ictrek image build and Feishu update entrypoint.
- `config/builtin_models.yaml` ships no deployment-specific model rows by
  default.
- prompt templates identify the assistant as `Vivibit AI小助手`.
- login and user menu links point to ictrek/Vivibit destinations.
- `docker-compose.override.yml` keeps local persistence and the required SSRF
  whitelist additions for model backends.

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
  docker-compose.override.yml docs/ictrek
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
