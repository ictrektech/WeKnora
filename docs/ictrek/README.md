# ictrek 运维说明

本目录保存 ictrek 对 WeKnora fork 的本地运维说明。中文说明放在上方，英文说明放在下方；涉及上游同步时，尽量把本地部署知识放在这里，避免常规同步覆盖。

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
- [远程 WeKnora 部署](remote-weknora-deployment.md)
- [Neo4j env 示例](neo4j.env.example)
- [上游同步](upstream-sync.md)

---

# ictrek Notes

This directory stores ictrek-specific operational notes for this WeKnora fork.

Keep these notes separate from upstream-facing docs so routine upstream syncs are less likely to overwrite local deployment knowledge.

Repository sources:

- ictrek fork: `git@github.com:ictrektech/WeKnora.git`
- upstream source: `git@github.com:Tencent/WeKnora.git`
- recommended upstream remote name: `upstream`

- Start with [Fresh host deployment](fresh-host-deployment.md) when bringing up
  a new host. Do not start from isolated commands.
- [Fresh host deployment](fresh-host-deployment.md)
- [Remote vLLM backend](remote-vllm-backend.md)
- [Ollama embedding backend](model-hub-ollama-embedding.md)
- [WeKnora image build](build-images.md)
- [Remote WeKnora deployment](remote-weknora-deployment.md)
- [Neo4j env example](neo4j.env.example)
- [Upstream sync](upstream-sync.md)
