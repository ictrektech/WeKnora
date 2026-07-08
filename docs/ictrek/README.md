# ictrek 运维说明

本目录保存 ictrek 对 WeKnora fork 的本地运维说明。涉及上游同步时，尽量把本地部署知识放在这里，避免常规同步覆盖。本文只保留中文说明。

仓库来源：

- ictrek fork：`git@github.com:ictrektech/WeKnora.git`
- 上游源仓库：`git@github.com:Tencent/WeKnora.git`
- 推荐的上游 remote 名称：`upstream`

文档入口：

- 第一次在空机器部署时，先看 [空机器部署总指南](fresh-host-deployment.md)，不要直接从零散命令启动服务。
- [空机器部署总指南](fresh-host-deployment.md)
- [远程 vLLM 后端](remote-vllm-backend.md)
- [Ollama embedding 后端](model-hub-ollama-embedding.md)
- [WeKnora 镜像构建](build-images.md)
- [ictrek 部署模板](deploy-template/)
- [并发和队列配置](deploy-template/CONCURRENCY.md)
- [手动触发未完成文档重解析脚本](deploy-template/trigger-reparse-incomplete.sh)
- [Orin NX / L4T 纯 Ollama compose overlay](deploy-template/docker-compose.orin-ollama.yml)
- [Orin NX / L4T 纯 Ollama env 示例](deploy-template/.env.orin-ollama.example)
- [Orin NX / L4T 纯 Ollama 模型 YAML 示例](deploy-template/config/builtin_models.orin-ollama.yaml.example)
- [远程 WeKnora 部署](remote-weknora-deployment.md)
- [Neo4j env 示例](neo4j.env.example)
- [上游同步](upstream-sync.md)
