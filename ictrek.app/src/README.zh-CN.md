# WeKnora

WeKnora 是企业知识库、RAG 问答、Wiki 知识图谱和智能体平台。本 VOS 包使用 pull 模式安装 WeKnora 四个镜像，并按所选 profile 启动对应 `ollama_server` 作为聊天、图片理解和 embedding 后端。

## 组件

- WeKnora Web 前端
- WeKnora App API
- DocReader 文档解析服务
- Agent Skills sandbox 镜像
- Ollama QA/VLM 容器
- Ollama embedding 容器
- 本地 Postgres 与 Redis

## Profile

安装时选择一个 profile：`AMD_with_cuda`、`ARM_with_cuda`、`l4t`、`thor_spark` 或 `SOPHON_bm1688`。

AMD 和 ARM 通用 profile 分别从 `AMD_with_cuda`、`ARM_with_cuda` 飞书表读取 WeKnora 与 `ollama_server` 镜像版本；`l4t`、`thor_spark`、`SOPHON_bm1688` 使用各自表格。

## 模型

本包不在镜像中写死默认模型。安装后需要通过 WeKnora UI 添加模型，或后续挂载模型配置文件。默认网络端点为：

- QA/VLM: `http://weknora-ollama-qa:11535/v1`
- Embedding: `http://weknora-ollama-embedding:11535/v1`

如由 Model Hub 预先管理模型，请确保对应模型已经存在于 Ollama 数据目录中。

