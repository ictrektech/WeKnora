---
name: rebase-main-linear
description: Keep a personal or feature branch synchronized with the local `main` branch by rebasing onto it and preserving a linear history. Use when the user asks to sync or update the current branch from `main`, wants the latest business commit at `HEAD`, wants to avoid `Merge branch 'main'` commits, or asks to clean up a merge-based synchronization. Do not use when the user explicitly requests a merge or asks to preserve merge topology.
---

# Linear Rebase onto `main`

将当前个人或功能分支变基到本地 `main`，保持线性历史，让最新业务提交位于 `HEAD`。默认不推送远程；遇到冲突或无法安全判断时暂停并报告。

## 适用边界

- 将“同步 `main`”“获取 `main` 最新代码”“不要出现 merge commit”“让业务提交保持在最顶端”等请求解释为优先使用 rebase。
- 尊重用户明确要求的 `merge`、保留合并拓扑或不改写历史的要求；这些情况不要套用本 skill。
- 使用仓库已有的本地 `main` 作为基线。只有用户明确要求同步远程最新状态时，才先按仓库约定 fetch；不要默默替换或猜测远程分支。
- 只操作当前非 `main` 分支。不要把 `main` 本身 rebase 到其他分支。

## 执行流程

### 1. 检查前置条件

- 运行 `git status --short --branch`。工作区或暂存区不干净时停止；不要自动 stash、丢弃或覆盖用户改动。
- 运行 `git branch --show-current`，确认当前分支不是 `main`，并记录当前 `HEAD` 哈希用于结果报告。
- 确认本地 `main` 存在：`git show-ref --verify --quiet refs/heads/main`。不存在时停止，不要猜测替代基线。
- 用 `git log --graph --decorate --oneline main..HEAD` 检查待保留的分支提交。若存在有独立语义的历史 merge commit，先报告并请求处理策略；若只是本次把 `main` 合入当前分支的同步 merge，可继续扁平化。

### 2. 执行线性同步

- 执行 `git rebase main`，不要执行 `git merge main`、`git pull` 或 `git reset --hard`。
- 保留当前分支的业务提交，并接受 rebase 导致的提交哈希变化。
- 出现冲突、空提交、需要 `--skip`，或 Git 无法明确判断提交归属时，立即停止。只读取并报告 `git status` 和冲突文件；不要自动 resolve、`git add`、`git rebase --continue`、`git rebase --skip` 或 `git rebase --abort`。

### 3. 验证结果

- 确认工作区干净：`git status --short --branch`。
- 确认 `main` 是当前 `HEAD` 的祖先：`git merge-base --is-ancestor main HEAD`。
- 确认同步后没有分支侧 merge commit：`git log --merges --oneline main..HEAD` 应无输出。
- 查看最终提交图：`git log --graph --decorate --oneline -12 main HEAD`；确认 `HEAD` 的 subject 是最新业务提交，而不是 `Merge branch 'main' ...`。
- 运行 `git diff --check main..HEAD`，检查变基后的差异没有空白错误。

## 汇报格式

- 简洁报告基线 `main` 哈希、变基后的 `HEAD` 哈希和 subject，并说明已移除同步 merge commit、工作区状态和验证结果。
- 明确说明 rebase 会重写当前分支提交哈希，并列出旧/新 `HEAD` 哈希（如可得）。
- 未经用户明确授权不要 push。若用户随后要求推送，先确认远程目标，再优先使用 `git push --force-with-lease`，不要使用裸 `--force`。
