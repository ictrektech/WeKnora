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

profile 按 `ollama_server` 的发布维度设置。WeKnora 自身 AMD 有无 CUDA 通用，ARM 有无 CUDA 通用，因此只查一个通用表；L4T、Thor Spark 和 Sophon 单独查表。

| profile | 飞书 sheet | 说明 |
| --- | --- | --- |
| `AMD_with_cuda` | `AMD_with_cuda` | x86_64 / AMD 通用 WeKnora + Ollama |
| `ARM_with_cuda` | `ARM_with_cuda` | ARM 通用 WeKnora + Ollama |
| `l4t` | `l4t` | Jetson / L4T |
| `thor_spark` | `thor_spark` | Thor Spark |
| `SOPHON_bm1688` | `SOPHON_bm1688` | Sophon BM1688 |

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

`scripts/update_version.sh` 用于发布自增版本：

```bash
./scripts/update_version.sh patch
```

脚本会：

1. 自增 `ictrek.app/VERSION`。
2. 通过 GitHub CLI 查询同组织下 `ictrektech/model_hub` 和 `ictrektech/pgv` 的 release assets，找到最新 `model_hub_*_pull.tar` 与 `pgv_*_pull.tar`，并写入 `manifest.yml` 的依赖版本。
3. 调用 `scripts/package.sh`，从飞书发布表读取 WeKnora 四镜像和 `ollama_server` 的最新版本，生成 `dist/weknora_${VERSION}_pull.tar`。
4. 提交 `VERSION` 和 `manifest.yml`，推送触发 tag `vos-weknora-v${VERSION}`。
5. 使用 `gh release create` 或 `gh release upload --clobber` 在 GitHub Releases 页面发布 tar 包。

CI 或本机执行前需要可用的 `gh` 登录态，以及飞书凭据 `~/.feishu.components.json` 或 `~/.feishu.json`。如依赖 release 不在默认同组织 repo，可用环境变量覆盖：

```bash
WEKNORA_MODEL_HUB_RELEASE_REPO=ictrektech/model_hub \
WEKNORA_PGV_RELEASE_REPO=ictrektech/pgv \
./scripts/update_version.sh patch
```
