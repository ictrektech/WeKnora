# ictrek 运维说明

本目录保存 ictrek 对 WeKnora fork 的本地运维说明。涉及上游同步时，尽量把本地部署知识放在这里，避免常规同步覆盖。本文只保留中文说明。

仓库来源：

- ictrek fork：`git@github.com:ictrektech/WeKnora.git`
- 上游源仓库：`git@github.com:Tencent/WeKnora.git`
- 推荐的上游 remote 名称：`upstream`

文档入口：

- 第一次在空机器部署时，先看 [空机器部署总指南](fresh-host-deployment.md)，不要直接从零散命令启动服务。
- 任意机器部署前，先看 [deploy-template/CONCURRENCY.md](deploy-template/CONCURRENCY.md)。它是模型大小、上下文长度、vLLM/Ollama 并发、聊天预留、后台队列权重和 Embedding 并发的统一参考。
- [空机器部署总指南](fresh-host-deployment.md)
- [远程 vLLM 后端](remote-vllm-backend.md)
- [Ollama embedding 后端](model-hub-ollama-embedding.md)
- [WeKnora 镜像构建](build-images.md)
- [ictrek 部署模板](deploy-template/)
- [部署模板脚本](deploy-template/deploy.sh)
- [并发和队列配置](deploy-template/CONCURRENCY.md)
- [手动触发未完成文档重解析脚本](deploy-template/trigger-reparse-incomplete.sh)
- [Orin NX / L4T 纯 Ollama compose overlay](deploy-template/docker-compose.orin-ollama.yml)
- [Orin NX / L4T 纯 Ollama env 示例](deploy-template/.env.orin-ollama.example)
- [Orin NX / L4T 纯 Ollama 模型 YAML 示例](deploy-template/config/builtin_models.orin-ollama.yaml.example)
- [远程 WeKnora 部署](remote-weknora-deployment.md)
- [Neo4j env 示例](neo4j.env.example)
- [上游同步](upstream-sync.md)

部署模板默认开启 `WEKNORA_REPARSE_INCOMPLETE_ON_START=true`。app 容器重建或重启后会扫描 `failed`、`pending`、`processing`、`finalizing` 文档并重新提交解析；启动扫描走 `critical` 队列，每条知识重新解析前会清理残留 queued/retry 任务。`deploy-template/deploy.sh` 会从飞书读取最新 `weknora`、`weknora-ui`、`weknora-docreader` 镜像写入 `.env`，执行 compose 后默认重建 `docreader` 和 `app`，再运行 `trigger-reparse-incomplete.sh` 补交失败/未完成文档。可用 `WEKNORA_RECREATE_DOCREADER_ON_DEPLOY=false` 或 `WEKNORA_TRIGGER_REPARSE_AFTER_DEPLOY=false` 跳过对应步骤。

如果改了 `docreader/` 里的解析逻辑，例如 PDF 文本层乱码检测、扫描页渲染策略、文档格式解析器，必须重新构建并部署 `weknora-docreader` 镜像，再重建 `docreader` 容器；只重启旧镜像不会生效。文档页工具栏的「重新解析失败文档」只扫描当前知识库 `parse_status=failed` 的文档并按当前默认解析配置批量重新提交；`pending`、`processing`、`finalizing` 由启动/部署脚本处理。

Graph 模板只提供实体、关系和示例文本配置，不会强制每个知识库生成 Graph。每个知识库可以单独关闭 Wiki/Graph，只保留向量/关键词检索；关闭 Graph 时前端会保留已配置模板，方便后续重新开启。
