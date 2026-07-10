# ictrek 运维说明

本目录保存 ictrek 对 WeKnora fork 的本地运维说明。涉及上游同步时，尽量把本地部署知识放在这里，避免常规同步覆盖。本文只保留中文说明。

仓库来源：

- ictrek fork：`git@github.com:ictrektech/WeKnora.git`
- 上游源仓库：`git@github.com:Tencent/WeKnora.git`
- 推荐的上游 remote 名称：`upstream`

## 普通用户先看

- [Vivibit AI 小助手用户使用指南](USERGUIDE.md)：创建知识库、配置模型和解析能力、上传/重析/下载文档、查看 Trace、使用知识库问答和 IM 集成。

## 文档入口

- 第一次在空机器部署时，先看 [空机器部署总指南](fresh-host-deployment.md)，不要直接从零散命令启动服务。
- 任意机器部署前，先看 [deploy-template/CONCURRENCY.md](deploy-template/CONCURRENCY.md)。它是模型大小、上下文长度、vLLM/Ollama 并发、聊天预留、后台队列权重和 Embedding 并发的统一参考。
- [空机器部署总指南](fresh-host-deployment.md)
- [远程 vLLM 后端](remote-vllm-backend.md)
- [Ollama embedding 后端](model-hub-ollama-embedding.md)
- [WeKnora 镜像构建](build-images.md)
- [ictrek 部署模板](deploy-template/)
- [部署模板脚本](deploy-template/deploy.sh)
- [已有部署一键更新脚本](deploy-template/update-and-deploy.sh)
- [并发和队列配置](deploy-template/CONCURRENCY.md)
- [手动触发未完成文档重解析脚本](deploy-template/trigger-reparse-incomplete.sh)
- [Orin NX / L4T 纯 Ollama compose overlay](deploy-template/docker-compose.orin-ollama.yml)
- [Orin NX / L4T 纯 Ollama env 示例](deploy-template/.env.orin-ollama.example)
- [Orin NX / L4T 纯 Ollama 模型 YAML 示例](deploy-template/config/builtin_models.orin-ollama.yaml.example)
- [远程 WeKnora 部署](remote-weknora-deployment.md)
- [Neo4j env 示例](neo4j.env.example)
- [上游同步](upstream-sync.md)

部署模板默认开启 `WEKNORA_REPARSE_INCOMPLETE_ON_START=true`。app 容器重建或重启后会先等待 `WEKNORA_REPARSE_WAIT_URLS` 中的模型服务 ready，再扫描 `failed`、`pending`、`processing` 文档；`finalizing` 只有在 `processed_at is null` 时才会整文档重跑。`deploy-template/deploy.sh` 从飞书读取三个 WeKnora 镜像，只替换镜像 digest 或部署配置发生变化的 frontend、app、docreader，不重启数据库和模型后端；发生相关替换后再运行 `trigger-reparse-incomplete.sh` 补交失败/未完成文档。

已有部署目录可运行 `./update-and-deploy.sh --platform amd|l4t|thor`。脚本从 `WEKNORA_DEPLOY_REPO` / `WEKNORA_DEPLOY_REF` 拉取最新部署模板，保留本机 `.env*`、`data/` 和模型配置，再调用 `deploy.sh` 检查飞书镜像并按需替换。

如果改了 `docreader/` 里的解析逻辑，例如 PDF 文本层乱码检测、扫描页渲染策略、文档格式解析器，必须重新构建并部署 `weknora-docreader` 镜像，再重建 `docreader` 容器；只重启旧镜像不会生效。文档页工具栏的「重新解析失败文档」只扫描当前知识库 `parse_status=failed` 的文档并按当前默认解析配置批量重新提交；`pending`、`processing` 和 `processed_at` 为空的 `finalizing` 由启动/部署脚本处理，已完成文字解析和向量入库的 `finalizing` 不会重复跑 docreader/embedding。

后台 housekeeping 在 app 启动时立即执行一次，之后每 5 分钟清理已经没有待完成工作的残留状态。即使 `pending_subtasks_count>0`，只要最新 attempt 没有 `pending/running` span、Asynq 也没有该知识的 queued/active 任务，就会把陈旧计数归零并推进为 `completed`；仍在排队或运行的多模态、Graph、Wiki、摘要、问题生成任务不会被清理。

启动时会先清理旧 attempt 和完全重复的 Asynq 任务，再按知识库开关清理已关闭的多模态/Graph 后台任务；重新开启多模态时，只有队列中不存在对应任务才补发 `image:multimodal`。日志可搜索 `startup-task-reconcile`。

Graph 模板只提供实体、关系和示例文本配置，不会强制每个知识库生成 Graph。每个知识库可以单独关闭 Wiki/Graph，只保留向量/关键词检索；关闭 Graph 时前端会保留已配置模板，方便后续重新开启。

Orin NX / L4T 纯 Ollama overlay 中的 `ollama-qa` 和 `ollama-embedding` 已显式配置 `runtime: nvidia`。如果部署后推理很慢，先用 `docker inspect ollama-qa --format 'runtime={{.HostConfig.Runtime}}'` 和容器内 `/dev/nvhost-gpu` 设备检查 GPU runtime，而不是先调并发。
