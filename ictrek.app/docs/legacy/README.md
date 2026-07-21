# Legacy 旧部署资料

本目录保存 HybRAG/WeKnora 早期独立部署、远程 compose、手工 vLLM/Ollama、旧 Neo4j 配置和部署模板资料。

这些资料只供排查旧环境、旧数据目录和历史安装方式时参考，不再作为当前部署入口，也不再随 VOS app 流程持续更新。

当前唯一维护的 VOS app 入口是 [`../../README.md`](../../README.md)。安装、升级、依赖版本、Model Hub 预热模型、PGV/Postgres 依赖、镜像选择、release 打包和 VOS 打开方式都以该文件为准。

## 归档内容

- `fresh-host-deployment.md`：旧空机器独立部署总指南。
- `remote-weknora-deployment.md`：旧远程 compose 部署与手工升级说明。
- `remote-vllm-backend.md`：旧 vLLM 后端手工启动说明。
- `model-hub-ollama-embedding.md`：旧 Ollama/model_hub embedding 手工部署说明。
- `deploy-template/`：旧独立 compose 模板、env 示例和辅助脚本。
- `neo4j.env.example`：旧 Neo4j env 示例。
