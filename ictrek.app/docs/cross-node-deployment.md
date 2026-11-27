# HybRAG 跨节点部署现状

HybRAG 留在当前 VOS 节点时，**模型推理、文档解析和智能体沙箱**优先考虑接入其他节点。这三类已有独立接口。批量入库也适合分担负载，前提是拆出独立 worker。

下一版优先让同一算力节点完成文档解析、OCR 和图片描述，再由主节点入库。 [算力节点设计](cross-node-docreader-routing.md)

| 组件 | 适合度与原因 | 当前接入方式与改造难度 | 验证状态 |
| --- | --- | --- | --- |
| 模型推理 | 高。问答、向量化、重排、视觉和语音可按模型分配节点，利用远端算力。 | **低，已支持。** 在模型设置填写兼容 API 地址、模型名和凭据。经 V-Scheduler 时，HybRAG 选用网关发布的模型。 [模型管理](../../website-docs/03-features/06-models.md) · [网关接入](model-backend-migration.md) | 开发环境已验证网关问答、向量和重排。VOS 跨机业务流程、同模型自动分流和故障切换待验证。 [既有验证记录](local-dev/README.md#v-scheduler) |
| 文档解析与 OCR | 高。大 PDF 渲染、Office 转换占 CPU 和内存，OCR 可按引擎利用 GPU。 | **DocReader 已支持，整套文档处理待实现。** VOS 统一完整安装。在「系统设置 → 算力节点」开启本机 DocReader 共享或用一次性码添加远端，按顺序或权重分配文件。自建 MinerU、PaddleOCR-VL 使用各自的接入配置。 [DocReader 配置](api/docreader-routing.md) · [解析架构](../../website-docs/03-features/03-document-parsing.md) | tc207 和 tc35 已验证配对、远端解析、入库、问答、顺序、权重和故障回退。扫描 PDF 的 OCR 和图片描述在实测中由 tc207 调用本机视觉模型，整套任务交给 tc35 待实现。 |
| 智能体沙箱 | 高。脚本、浏览器和文件处理可在独立节点消耗 CPU、内存和磁盘。 | **低至中，已支持。** 可配置远端 Docker、CubeSandbox 或 E2B。远端 Docker 需要 TLS。 [沙箱配置](../../website-docs/03-features/22-skills-sandbox.md) | 远端配置已核对。目标 VOS 上的工具执行、文件上传及产物回传待验证。 |
| 后台入库与知识加工 | 批量入库时较高。数据同步、切分、写索引和重解析可分担主应用的负载。 | **中至高，需拆服务。** 已有 Redis/Asynq 队列，worker 随主应用启动。需增加独立 worker 启动方式，并共享数据库、文件及任务状态。 [启动代码](../../internal/container/container.go) · [队列配置](../../internal/router/task.go) | 独立 worker 部署待实现。验收覆盖重试、取消和任务恢复。 |
| 索引与检索 | 有条件适合。大库和高并发更依赖内存、磁盘 I/O 和网络延迟。应用侧结果融合与去重可留在主应用。 | **中，支持外部引擎。** 可配置远端 PGV 或已支持的检索引擎。存量库切换引擎需考虑数据迁移或重建索引。 [检索引擎](../../website-docs/03-features/05-retrieval-engines.md) | 已有 PGV 关键词与向量查询验证。远端延迟、吞吐与切换后的召回待验证。 [PGV 验证记录](../README.md) |
| 数据库、Redis、文件存储、Neo4j | 适合稳定的数据服务节点。主要收益是容量和 I/O 隔离。 | **中至高。** PostgreSQL、Neo4j 和外部对象存储已有连接配置。VOS 的 Redis 地址需改 Compose。迁移存量服务还需安排数据切换与回滚。 [VOS Compose](../src/docker-compose.yml) · [存储后端](../../website-docs/03-features/19-storage-backends.md) | 接入配置已核对。跨节点数据迁移、切换和恢复待验证。 |
| 外部搜索与工具服务 | 有条件适合。收益取决于外部工具本身的负载。 | **低，已有接入。** 自建 SearXNG 可填写 Base URL，MCPHub 可配置服务地址。 [网络搜索](../../website-docs/03-features/11-web-search.md) · [VOS Compose](../src/docker-compose.yml) | 地址配置已核对。目标节点的网络、鉴权及工具调用待验证。 |
| 主应用、页面与业务编排 | 单独拆分收益较低。登录、权限、会话和流程编排依赖共同状态。 | **整套迁移沿用 VOS 部署。** 单独迁移编排属高难度改造，需要拆服务并定义状态边界。 [部署手册](../README.md) | 编排独立部署待设计。 |

## 安装与依赖

tc207 和 tc35 均采用 0.1.87 ARM64 候选包完整安装，PGV 继续由 VOS 按应用检查。DocReader 共享在 HybRAG 内启用并授权，默认关闭。组件安装曾在早期预检包中验证，按组件声明外部依赖可留作 VOS 平台后续设计议题。 [安装清单](../src/manifest.yml)
