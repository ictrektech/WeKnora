# 上游同步

本文面向 ictrek fork（保留本地定制的派生仓库）维护者，是 Tencent/WeKnora 上游同步流程的唯一事实源。先固定两端提交，审查本地兼容行为及测试，再合并、验证和提交。下方附英文流程。

| 项目 | 当前要求 |
| --- | --- |
| 支持 | 人工审查本地定制、兼容契约（已有配置必须保持的行为）及测试迁移 |
| 必需 | 保留定制行为及已执行迁移历史；迁移变更须验证空库安装与存量升级 |
| 未实现 | 自动发现全部 fork 定制、低编号新增迁移及存量升级回归的专用 CI 门禁 |
| 下一步 | 固定提交，审查本地兼容与迁移历史，合并后完成对应验证再提交 |

## 2026-10-29 上游整合记录

已整合并保存本地提交：将 Tencent/WeKnora `40192ace214ca046a86b590d69c1fe7286f3423e` 的 17 个新增提交合入 `main`，合并前为 `72b11e4130a67370f8332caa2ddfb9ba008587b2`，共同基线为 `256a3f2966866337f7dc3c5fcc10037f2ae18d52`。本轮保存本地合并及父仓库引用；未推送、未发布、未重启部署。下一步是发布前核验真实模型、DeepSeek 服务和沙箱桌面，再按授权交付。

### 行为与兼容

| 范围 | 合并结果与依据 |
| --- | --- |
| 默认设置 | 合入空间默认对话、辅助、向量、重排、视觉与语音模型及设置页面。成员可读、管理员可写；显式资源模型优先。保留本地会话标题的回答模型优先级和失败摘要回退，再接入空间辅助模型回退。默认模型与标题服务回归通过。 |
| 搜索与模型 | 合入 DeepSeek 搜索及网页读取、DSML（模型输出中的工具调用标记）恢复校验、Ollama 流结束原因、视觉响应大小限制和最终回答显式关闭思考。模型配置解析、流式及非流式请求的旧 `thinking_control=think`、`thinking_control=reasoning_effort` 契约及脱敏回归随根模块测试通过。默认最终回答预算 `2048` 及环境覆盖保留。 |
| 沙箱与技能 | 合入按会话分配桌面、浏览器登录状态共享及内置浏览器技能。共享机器上的目录仍不是操作系统隔离；真实镜像和桌面运行待验证。 |
| 界面与文档 | 合入助手引导、默认设置和上游 `v0.9.0` 文档。三种语言按键整合，保留本地翻译及工作台；数据库说明保留 fork 编号并补齐本轮映射。同步记录的旧沙箱锚点改为 `#user-sandbox`。 |
| 本地定制 | 构建入口、VOS 打包与镜像配置、compose 持久化及 `host.docker.internal` 白名单、空内置模型、品牌提示词、登录页、用户菜单保持原样；保留个人模型、脱敏、法律与专利工作台、归档只读和检索范围。 |

双方共同修改的 43 个文件已审查。上游没有删除或重命名文件，未删除本地测试；网页读取改用解析后的服务客户端，调用方及测试同步迁移。实际冲突为英、日、繁中翻译、`session.go`、SQLite 结构测试和数据库说明共 6 个文件，分别保留本地键、标题回退及 fork 历史，合入上游通用逻辑。

### 迁移与验证

合并前及上一发布标签 `vos-hybrag-v0.1.77` 的 PostgreSQL 历史上界均为 `156`、SQLite 均为 `61`。两份基线各自的 314 个 PostgreSQL 和 124 个 SQLite 迁移文件已逐字节比对，路径、编号和内容不变。新增空间默认设置迁移追加为 PostgreSQL `000134` → `000157`、SQLite `000051` → `000062`；没有补偿迁移。

`TestDefaultSettingsMigrationUpgrade` 使用临时 SQLite 文件和独立 PostgreSQL 17 容器，分别覆盖空库及上一发布版 `61`／`156` 升级。原始迁移前缀已与发布标签逐字节核验。最终为 SQLite `62/dirty=false`、PostgreSQL `157/dirty=false`；`tenants.default_settings` 分别为可空 `TEXT`／`JSONB`，旧行保持空值，空间名称、业务字段及助手配置保留，模型设置写入和读取一致，再次升级无待执行项。

既有 `TestSandboxAutomationMigrationUpgrade` 同步更新最终版本期望，继续覆盖 SQLite 空库及 `47`、`55`、`59`，PostgreSQL 空库及 `141`、`150`、`154` 升级；任务、会话、关联、字段、索引及主键回归通过。新增 `TestDefaultSettingsHTTPPersistsAfterUpgrade` 在 SQLite `61` 升级后使用实际 HTTP 处理函数、服务和仓库验证默认设置保存、清空和重读；模型目录使用替身，未验证真实模型服务。PostgreSQL 接口链路和部署数据库版本仍待验证，没有读取或迁移业务库。

已验证：两套迁移文件配对、历史不可变、`git diff --check`，根模块 `WEKNORA_LOG_FORMAT='' go test -tags sqlite_fts5 ./...` 和 `go vet -tags sqlite_fts5 ./...`。前端类型检查、生产构建通过，单测 2,443 项中 2,442 项通过、1 项条件跳过；构建仍有静态脚本及大分包提示。MCP（外部工具接入协议）聊天流与异步预检使用已有虚拟环境的 `unittest`，31 项通过。

模拟登录后的聊天页到知识库、智能体、设置、新对话及其他会话跳转通过，无未处理的 JavaScript 或 Vue 错误。浏览器整套回归首轮被新增助手引导遮挡，两个本地工作台测试的初始化补入该引导完成标记，未改产品逻辑或放宽断言；禁用失败重试复跑，39 项全部通过。桌面浏览器同步和请求标识脚本的 25 项 Node 测试通过；`bash docker/scripts/tests/run.sh` 的脚本语法及全部断言通过，覆盖会话占用、隔离、回收、故障恢复和浏览器状态共享。

待验证：部署数据库版本及备份副本升级、PostgreSQL 真实接口写入、外部模型与 DeepSeek 搜索、真实沙箱镜像与桌面登录共享、镜像构建及 VOS 安装。上述隔离与模拟测试不能证明目标部署已通过。

## 2026-10-27 上游整合记录

已整合：将 Tencent/WeKnora `256a3f2966866337f7dc3c5fcc10037f2ae18d52` 的 106 个新增提交合入 `main`，合并前为 `258b1e20d4a9810cc1b88afdb2b90c921edec1fd`。先快进纳入 fork 远端的 2 个提交，保留共享 PGV 依赖的发布来源修复。下一步是发布前验证真实接口、模型、外部存储和目标部署；源码合并不代表已发布或已部署。

### 行为与兼容

| 范围 | 已实现的结果与验证依据 |
| --- | --- |
| 聊天与智能体 | 合入失败流草稿清理、历史加载重试、最终回答上下文压缩与错误上抛、文本模型的上传图片描述。保留本地跨会话流隔离、工具来源角色、图片要求、默认 `2048` 个 token（模型计数单位）的最终回答预算及 `WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS`。旧调用方的无会话参数形式继续兼容，现场会话使用上游压缩和单次溢出重试；相关智能体与会话测试通过。 |
| 知识库与检索 | 合入批量重析分批轮询、过期响应隔离、关闭问题生成时的检索过滤、图谱检索范围校验及数据源事务替换。保留托管归档只读、处理耗时字段和本地后台模型并发门禁；生成问题先去掉思考段，仍将无可用问题判为失败。批量重析 39 项和根模块服务测试通过。 |
| 文档解析 | 合入 PDF（便携式文档格式）数值行保留、DOCX（Word 文档格式）内容控件和修订插入、电子表格标题去重与编码识别修复。保留 PDF 乱码识别及来源定位；DOCX 的直接段落保留原编号，包装段落使用独立编号，表格单元格来源范围包含控件与修订文本。新增来源编号、范围及单元格断言；相关回归通过。 |
| 界面 | 合入按页面推荐的助手入口、知识库目录和标签布局、模型编辑器层级及 Lite（本机运行版本）命令授权。保留个人模型标题、工具栏开关、会话流隔离、归档权限及处理耗时。三种有冲突的语言按键整合，其余三种语言保留自动合并结果；类型、单测、构建及模拟登录跳转通过。 |
| 容器与模型 | 合入 Docker Desktop 的 socket 权限兼容，将初始化统一放到 `main`，测试载入脚本时不执行启动操作。保留 `/data` 权限、内置技能补齐、运行时模型生成和 `WEKNORA_RUN_AS_ROOT`；入口测试及 11 项模型后端测试通过。后端镜像新增 `qpdf` 依赖以支持 MinerU 云端 PDF 分片，尚未构建镜像。 |
| 其他上游改动 | 合入模型事件流多行分帧、工具参数字符串保留、拒绝文本、图谱证据范围、配额解析及客户端下载等修复；根模块、客户端、命令行和 MCP（外部工具接入协议）服务回归覆盖相应路径。 |

共同基线为 `97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9`，双方共同修改的 79 个文件已审查。上游未删除或重命名文件，也没有删除本地定制测试；移除的旧图谱原始请求范围解析由规范化 `SearchTargets` 及范围回归替代，未使用的 Wiki 字符串辅助函数随上游删除。

实际冲突共 17 个文件：`Makefile`、DOCX/PDF 解析器、模型编辑器、聊天流处理、英/日/繁中翻译、`AgentStreamDisplay.vue`、`botmsg.vue`、聊天页、知识库页、`finalize.go`、`knowledge_process.go`、会话标题测试、流式资源测试及容器入口脚本。分别保留本地测试标签、来源信息、界面与模型配置、流隔离、预算和失败行为，同时接入上游通用修复。

构建发布入口、VOS（应用运行平台）打包模板、前端镜像入口、compose（容器编排）覆盖文件、空内置模型、品牌提示词、登录页及用户菜单保持不变。基础 compose 只追加构建元数据、配额及 VLM（图片理解模型）预算配置，持久化和 `host.docker.internal` 白名单保留。会话标题模板采用上游更短的侧栏标题要求；模型运行时、OpenAI 请求适配和聊天脱敏实现未变，旧思考参数契约仍由根模块测试覆盖。

### 迁移与验证

本轮没有新增、修改、删除或重编号迁移。合并前与上一发布标签 `vos-hybrag-v0.1.76` 的 PostgreSQL（服务端关系数据库）历史上界均为 `156`，SQLite（轻量文件数据库）均为 `61`。两份基线各自的 314 个 PostgreSQL、124 个 SQLite 迁移文件已逐字节比对，路径与内容均未变，没有新旧编号映射。未读取部署数据库的 `version/dirty`（迁移编号及中断标记），未另做隔离空库或存量升级验收，不据此宣称生产升级通过。

已验证：两套迁移配对、工作区与暂存区 `git diff --check`、根模块 `WEKNORA_LOG_FORMAT='' go test -tags sqlite_fts5 ./...` 与 `go vet -tags sqlite_fts5 ./...`、`client/` 和 `cli/` 的 `go test ./...`。首轮根模块仅最终回答的新增预算断言失败；该断言改为显式设置 `4096` 预算，继续验证上游的思考字段不重复计数，保留产品默认值，随后根模块全量复跑通过。

前端 `npm run type-check`、`npm run build-only` 和 `npm test` 通过：2,424 项中 2,423 项通过、1 项因浏览器环境条件跳过。新增批量重析测试补入本地智能归档接口的模拟依赖，39 项通过。生产构建仍有静态脚本和大分包提示。`npm run test:e2e -- --retries=0` 的 39 项全部通过；另在独立开发服务使用模拟登录和接口，验证聊天页到知识库、智能体、设置、新对话及其他会话的跳转，无未处理的 JavaScript 或 Vue（前端界面框架）错误。

文档解析使用已有虚拟环境执行 `pytest`，排除下述两个环境用例后，481 项通过、13 项条件跳过、2 项显式排除；另有 226 项子用例断言通过。新增 DOCX 来源范围断言通过；同时修正一个合并前已存在的来源单元测试预期：范围 `0:4` 对应“付款方式”，原预期误写为“付款”，合并前快照复现同样失败。`mcp-server/` 使用自己的虚拟环境运行聊天流与异步预检，共 31 项通过。容器入口权限检查和 `ictrek.app/scripts/test_model_backend.py` 的 11 项均通过。

待验证的环境用例：LibreOffice（Office 文档转换工具）在独立用户配置下仍无法加载最小生成样例，未生成 `.xls`，该测试与合并前逐句一致；WebKit 浏览器已下载，但本机缺少其系统动态库，SSRF（服务端请求访问限制）代理的 WebKit 集成用例无法启动。两项未改为跳过或降低断言。13 个条件跳过用例仍受样本、可选工具等环境限制；这些结果不能记为完整文档解析集成测试通过。

待验证：真实接口与数据库写入、外部模型和存储、真实本机沙箱授权与共享沙箱、数据库备份副本升级、镜像构建和目标部署。本轮没有发布镜像或 VOS 包，也没有重启服务。

## 2026-10-26 本地整合记录

已整合到 `main` 并于 2026-10-27 按授权保存为本地合并提交：Tencent/WeKnora `97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9` 的 46 个新增提交，合并前为 `979da15c0c66cf74deac67396e8ada6e4031fd60`。未推送、未更新父仓库引用。下一步是审阅合并提交与下列验证边界，再按授权推送或部署。

### 行为与本地兼容

| 范围 | 已实现的结果与边界 |
| --- | --- |
| 文档解析 | PDF（便携式文档格式）扫描页按需流式传输，按固定批次关闭渲染器；原生文本页内嵌图先筛选再解码保留图。保留本地乱码文本层检测、默认 `500` MiB（500 × 1024 × 1024 字节）请求上限及来源定位；强制扫描的新流式路径补接来源范围，传输元信息继续使用 `_response_metadata`。机制、失败回退和资源边界见 [DocReader 资源控制](../../docreader/README.md#解析器资源控制)。同时合入损坏 PDF 回退、表格预算、电子书外链图片及表格文件修复。 |
| 检索 | 按模型名称、端点和配置维度分组复用查询向量，避免不同维度知识库互相干扰。关键词检索失败但向量检索成功时保留结果并记录降级；没有可用结果的失败仍返回错误。正文高度重叠但各有独立事实时保留两项；引用替换保留 JSON（结构化数据文本）字符串转义。相关回归随根模块测试运行。 |
| 智能体与模型 | 合入上下文超限恢复、成功压缩后清除耗尽标记及重复失败工具调用提示。保留本地首次失败后跳过相同调用的规则，新上游测试改为断言实际工具只执行一次；提示仍在连续三次失败结果后出现。Claude 自适应思考请求新增 `display=summarized`，本地显式零温度及旧思考参数兼容保留。 |
| 问题生成 | 单次补全预算默认从 `512` 提高到 `2048` 个 token（模型计数单位），可用正整数 `GENERATE_QUESTIONS_MAX_TOKENS` 覆盖；非法或非正值回退默认值。保留本地后台模型并发限制和空结果报错行为，全部模型调用失败会使任务失败；仅部分失败时保留成功结果并记录 `partial_failure`。预算及空结果回归按本地契约调整。 |
| 界面与沙箱 | 合入检索设置保存期间编辑保留、成员搜索过期响应隔离和处理耗时展示。只读用户隐藏解析设置提示，本地托管档案仍只读。共享沙箱不再因技能镜像变化自动重建；旧机器复用和主动释放的边界见[沙箱分配方式](../../website-docs/03-features/22-skills-sandbox.md#user-sandbox)。 |
| 客户端及工具服务 | 客户端错误响应最多读取 `1` MiB；命令行重试保留修订参数与理由，环境凭证去除首尾空白。外部工具服务把聊天前的同步元数据请求移出事件循环；受控本地 HTTP（网络请求协议）服务的异步回归通过。 |

共同基线为 `314a6fafd718724b5e6a11b42650b51db6b588d8`。双方共同修改的 24 个文件已逐项审查；上游本轮无删除或重命名文件，未删除本地定制测试。实际冲突为 `docreader/main.py`、`docreader/parser/pdf_parser.py`、`frontend/src/i18n/locales/zh-TW.ts`、`frontend/src/views/knowledge/KnowledgeBase.vue` 和 `website-docs/03-features/22-skills-sandbox.md`：分别保留来源信息、乱码检测、繁中本地文案、归档权限和旧目录升级说明，并合入上游功能。新增检索测试移除已被本地构造函数删除的会话服务参数；新增配置测试沿用本地 `500` MiB 默认值。

构建入口、前端镜像文件、compose（容器编排）及其覆盖文件、空内置模型、提示词、登录页、用户菜单与合并前一致；持久化和 `host.docker.internal` 白名单保留。模型运行时、OpenAI 请求适配、聊天脱敏包装保持不变，原 `thinking_control=think` 与 `thinking_control=reasoning_effort` 契约继续由根模块测试覆盖。

### 迁移与验证

本轮没有新增、修改、删除或重编号迁移。合并前与上一发布标签 `vos-hybrag-v0.1.76`（`a13851175`）的 PostgreSQL（服务端关系数据库）上界均为 `156`，SQLite（轻量文件数据库）均为 `61`。逐字节比对这两个基线中的全部迁移路径与内容均未变，没有新旧编号映射。未读取目标部署的 `version/dirty`（迁移编号及中断标记），未另做隔离空库和存量升级验收，不据此声称生产数据库升级通过。

已验证：两套迁移配对检查、工作区与暂存区 `git diff --check`、根模块 `WEKNORA_LOG_FORMAT='' go test -tags sqlite_fts5 ./...` 和 `go vet -tags sqlite_fts5 ./...`、`client/` 与 `cli/` 的 `go test ./...`。初次根模块检查暴露上述新增测试的构造函数参数及空结果期望差异，调整后全量复跑通过；问题生成预算与空结果的定向复跑也通过。

前端 `npm run type-check`、`npm run build-only` 通过；`npm test` 共 2,238 项，2,237 项通过、0 项失败、1 项因浏览器环境条件跳过。生产构建仍有静态脚本及大分包提示。浏览器整套 `npm run test:e2e -- --retries=0` 共 39 项全部通过。另用独立开发服务和模拟接口验证聊天页到知识库、智能体、设置、新对话及其他会话的跳转，无未处理的 Vue（前端界面框架）或 JavaScript 错误；首次人工脚本的组织列表响应结构不符造成界面错误，修正模拟响应后通过，未修改产品代码。

DocReader 使用已有虚拟环境运行本轮受影响模块及来源、请求内存回归，共 151 项，147 项通过、4 项因缺少大文档样本或 ImageMagick（图片转换工具）跳过。新增断言覆盖强制扫描及自动路由的普通与流式来源范围一致，以及流式元信息传输。外部工具服务 `test_chat_async_preflight` 的 9 项回归通过。

待验证：真实接口与数据库写入、外部模型和存储、共享沙箱主动释放与重建、真实大文档内存峰值与图片识别、目标部署。模拟接口和隔离单元测试不证明上述环境通过；本轮没有构建或发布镜像、重启远程服务。

## 2026-10-23 第二轮本地整合记录

已合入并以本地合并提交保存：将 Tencent/WeKnora `314a6fafd718724b5e6a11b42650b51db6b588d8` 的 36 个新增提交整合到 `main`，合并前提交为 `99caae416f2fd9858dc0e99a0b5b8bb0dd2074d4`。未推送、未更新父仓库引用。下一步是审阅验证限制，再按授权推送或部署。

### 行为与本地兼容

| 范围 | 已实现的结果与边界 |
| --- | --- |
| 图片入库 | 内联图片先存储再分块，按正文顺序分配每种语法前 30 张的识别预算；超额图片仍保存，但跳过 OCR（图片文字识别）和描述。无法确认图片边界或残留图片编码时终止解析并清理未绑定上传。行为及操作见[解析配置](USERGUIDE.md#7-解析配置)，回归见 `image_resolver_test.go` 和 `knowledge_process_inline_image_test.go`。 |
| 文档解析 | 扫描 PDF 的页图编码后逐项释放原始字节；文件请求达到 16 MiB（16 × 1024 × 1024 字节）时，在 RPC（跨进程服务调用）的工作线程返回后回收循环引用，并在支持的 Linux 环境调用 `malloc_trim` 归还空闲内存。它不保证进程内存回到初始值，也不改变上传限制。EPUB（电子书格式）章节保留列表、表格和引用结构。对应内存、章节及来源单元测试通过。 |
| 图片预处理 | 缩小图只用于检测，裁切回到原图，再按输出边长限制缩小；默认开关和资源不足时回退原图的行为保留。探测图参数为 `DOCREADER_PREPROCESS_PROBE_EDGE`，见[解析配置](USERGUIDE.md#7-解析配置)。 |
| 文档与数据源 | 数据源同步遵守跳过冲突策略；重新解析成功写入替代分块及索引后，才清理被替代的软删除分块。向量索引分窗口保存，降低峰值内存。保留本地托管归档只读限制、解析产物复用及后台模型并发限制；根模块服务和检索测试通过。 |
| 界面 | 合入 FAQ（常见问答）列表视图、窄屏登录布局、文档工具栏换行、批量重析保留已加载列表，以及图片画廊取消过期请求和图谱隐藏时跳过布局。保留 VOS（应用运行平台）自动登录、Vivibit 链接、归档权限和本地操作按钮。 |
| 模型与智能体 | 合入无索引流式工具调用的独立编号、上下文耗尽和安全结束原因兼容，以及多模态消息清理修复。保留 `thinking_control=think`、`thinking_control=reasoning_effort` 的旧配置映射和脱敏失败阻断；根模块测试覆盖 `runtime`、`openaicompletions` 及 `chat` 包。 |
| 日志与测试钩子 | 新变量 `WEKNORA_LOG_FORMAT` 未设置时回退到旧 `LOG_FORMAT`，显式空值禁用回退。唯一冲突在 `scripts/git-hooks/common.sh`：同时保留上游日志隔离变量与本地 `sqlite_fts5` 测试标签；钩子回归通过。 |

双方共同修改的 22 个文件已逐项审查。上游没有删除或重命名文件，也未删除本地定制测试。构建入口、前端镜像文件、compose 覆盖、空内置模型、提示词、用户菜单及模型运行时和脱敏包装与合并前一致。基础 compose 仅更新日志变量并追加预处理参数，持久化及 `host.docker.internal` 白名单保留。`ictrek.app/` 只同步用户指南、本地调试说明与本记录。

### 迁移与验证

本轮没有新增、修改、删除或重编号迁移。合并前 PostgreSQL（服务端关系数据库）最高编号为 `156`，SQLite（轻量文件数据库）为 `61`；上一发布标签 `vos-hybrag-v0.1.75`（`ae4812de8ee955b8580be8306be22444dec8178f`）分别为 `155`、`60`。逐字节核对两份历史的全部迁移路径和内容不变，没有新旧编号映射。未读取目标部署的 `version/dirty`（迁移编号及中断标记），本轮未另做隔离空库或存量升级验收，不据此宣称生产数据库升级通过。

已验证：两套迁移配对、工作区及暂存区 `git diff --check`、`bash scripts/test-git-hooks.sh`、根模块 `WEKNORA_LOG_FORMAT='' go test -tags sqlite_fts5 ./...` 和 `go vet -tags sqlite_fts5 ./...`。前端 `npm run type-check`、`npm run build-only` 通过；`npm test` 共 2,113 项，2,112 项通过、0 项失败、1 项因浏览器环境条件跳过。构建仍有静态脚本和大分包提示。

DocReader 使用现有虚拟环境运行配置、请求内存、EPUB、PDF、预处理和来源单元共 127 项测试，124 项通过、3 项跳过。测试包含本地服务的大文件请求内存回收；预处理几何测试使用替身识别器，不能证明真实 OCR 效果。

使用模拟登录与接口，在独立开发服务验证聊天页到知识库、智能体、设置、新对话和其他会话的跳转通过，未见未处理的 Vue（前端界面框架）或 JavaScript 错误。浏览器整套回归 39 项中 35 项通过、4 项失败；失败项为专利工作台的 2 项初始跳转超时、1 项示例消息数断言，以及 VOS 语言桥接重复订阅断言。这 4 项在独立开发服务上串行复跑全部通过，期间未修改产品代码或用例。失败与运行环境、时序有关仍属原因推断，不能将整套浏览器回归记为稳定通过。

后续合并前后对照：从合并前 `99caae416f2fd9858dc0e99a0b5b8bb0dd2074d4` 导出前端与示例数据快照，与合并后工作区使用同一套已安装依赖和浏览器；分别启动独立开发服务、独立依赖预编译缓存，以 2 个测试进程、禁用失败重试顺序运行整套 39 项。合并前为 37 项通过、2 项失败，失败为上述两个初始跳转超时；合并后为 38 项通过、1 项失败，仅初始专利页面跳转超时。这两项超时已证实在本轮合并前存在。示例消息数与语言桥接断言在两端均通过，尚未确定首次失败的原因，不能据此归因于上游或断言已修复。未发现本轮上游合并新增且在对照中可复现的失败。

后续修复已实现：浏览器测试默认启动独立的 `5174` 服务，限制为 2 个测试进程，界面断言窗口由 5 秒改为 15 秒，容纳 Vite（源码开发服务）首次按需编译组件。复查语言用例的网络记录发现，它直接导入 `/src/i18n/index.ts`，应用依赖却引用带热更新参数的同一路径，形成两个语言模块实例并重复订阅。修复让语言和模型编辑器测试页面复用应用入口返回的完整模块地址；未修改应用运行逻辑。默认入口与已有服务的使用方式见[浏览器回归](local-dev/README.md#浏览器回归)。

修复验证：同一个曾稳定复现重复订阅的开发服务上，语言与模型编辑器用例连续 3 轮共 12 项全部通过；独立服务使用全新编译缓存，整套 39 项全部通过；默认入口将原 4 项失败用例连续运行 3 轮，共 12 项全部通过。以上均禁用失败重试，没有放宽断言内容。前端类型检查和差异格式检查通过。示例消息数首次失败未单独定位到确定原因，本次在扩大等待窗口与隔离服务后未再复现；这些结果不保证所有机器上永不超时，也不替代真实接口验收。

待验证：真实接口写入、外部模型与存储、沙箱重绑定、实际图片识别效果和目标部署。模拟接口与替身测试不证明上述环境通过。本轮没有构建或发布镜像、重启服务。

## 2026-10-23 首轮本地整合记录

本次目标为 `main`：将 Tencent/WeKnora `bc1ade36b933ff7689f1659d670cfd5683c174c5` 的 27 个新增提交合入 fork `135ed222bda317917c7865ff1f94d86212c89202`。冲突已解决，以本地合并提交保存；未推送，也未更新父仓库引用。下一步是审阅下列验证限制，按授权范围决定推送及部署。

### 行为与本地兼容

| 范围 | 已实现的结果及边界 |
| --- | --- |
| 后台任务 | 新增 `shell_exec_async`、`work_wait`、`work_cancel` 及聊天页任务入口。停止当前回答不会取消异步 Shell（命令执行进程）；应通过任务入口或取消工具停止。任务活跃时拒绝会话分支和回退，避免快照与任务写入竞争。真实沙箱运行仍待验证。 |
| 本地工作台 | 保留法务、专利会话归属、明确选取资料的检索范围、记忆限制和输入模型恢复。为本地两种工作台的会话清理补接后台停止门禁；停止失败时保留会话记录并重新允许任务提交，已清理附件不回滚。三种会话归属的成功、失败及其他工作台保留回归通过；见[法律工作台](legal-workspace.md#数据删除)和[专利工作台](patent-workspace.md#会话配置与数据边界)。 |
| 图片处理 | 合入 OCR（图片文字识别）前的可选摆正和裁剪；旧知识库及默认配置保持关闭。子进程忙碌、超时、超出内存或图片大小限制时回退原图。参数改用流水线所属键；旧 `image_actions.ocr.on_unobserved` 不再被读取，需在新面板重新确认观察失败后的 OCR 设置。实现及限制见 [DocReader 说明](../../docreader/README.md#ocr--vlm)。 |
| 其他上游修复 | 合入共享知识库模型按所属空间解析、资源链接按实际存储实例签发、克隆回滚保护共享资源、数据源日志保留、重排响应兼容及引用定位修复。对应上游回归随根模块测试通过；未用真实模型和外部服务验收。 |
| 冲突处理 | 解决 `docreader/uv.lock`、聊天流处理、实体提取及英/日/繁中翻译共 6 个文件的冲突。保留本地聊天流恢复和简单查询免模型处理，加入后台事件与模型所属空间上下文；翻译按键合入。依赖锁保留阿里云镜像及已有版本约束，重新解析后校验通过。另移除上游新增的同名 `ptrTime` 定义，复用本地现有函数。 |
| 部署及模型定制 | 构建入口、前端镜像文件、compose 覆盖、空内置模型、提示词、登录页和用户菜单与合并前一致；基础 compose 保留持久化及 `host.docker.internal` 白名单，只追加预处理配置。模型脱敏及思考适配代码未变，相关回归包含在根模块测试内。 |

共同修改的 53 个文件已审查；上游本轮没有删除或重命名文件，也未删除本地定制测试。`ictrek.app/` 仅补充本次同步和工作台清理说明，打包及发布配置保持不变。

### 迁移与验证

本轮没有新增、修改、删除或重编号数据库迁移。合并前 PostgreSQL（服务端关系数据库）最高编号为 `156`，SQLite（轻量文件数据库）为 `61`；上一发布标签 `vos-hybrag-v0.1.75`（`ae4812de8`）分别为 `155`、`60`。逐字节核对上述两份历史的全部迁移路径及内容均未改变。未读取目标部署的 `version/dirty`（迁移编号及中断标记），没有对业务库执行迁移，也未另做 PostgreSQL 空库或存量升级验收。

已验证：两套迁移文件配对、`git diff --check`、根模块 `go test ./...`；新增清理门禁后另跑服务包全量及定向回归。前端类型检查、生产构建通过；翻译键、运行时引用及 FAQ（常见问答）批量操作的 20 项定向测试通过。DocReader 配置、预处理执行器、几何处理及文档表格共 73 项测试通过，几何测试使用替身识别器，未加载真实 OCR 模型。

前端全量单测共 2,071 项：2,064 项通过、6 项失败、1 项因浏览器环境条件跳过。6 项失败在合并前快照中同样复现：样式字号计数 `219` 超过基线 `208`，以及 `browserSource.test.ts` 的 5 项测试因 Node 环境没有 `window` 而失败。本次不修改这些既有问题。

浏览器现有回归首次运行 15 项通过、VOS（应用运行平台）语言桥接 1 项失败；独立运行该语言测试通过。独立开发服务的整套运行另出现冷启动超过 5 秒的跳转超时，合并前快照也复现冷启动超时；不能将整套浏览器回归记为稳定通过。另用模拟登录和接口完成聊天页到知识库、智能体、设置、新对话及其他会话的跳转，未见未处理的 Vue（前端界面框架）或 JavaScript 错误。构建仍有静态脚本及大分包提示。

待验证：真实接口与数据库写入、远端沙箱和 Lite 本机异步任务、真实图片预处理效果、外部模型和存储、目标部署及生产数据库备份副本。模拟接口、替身识别器和现有迁移回归不能证明这些环境通过；本轮没有构建或发布镜像、重启服务。

### 前端检查失败的后续修复（2026-10-23）

已实现：修复 [Frontend 失败任务](https://github.com/ictrektech/WeKnora/actions/runs/39884926135/job/120190756328)暴露的上述 6 项失败。根因均来自专利工作台提交 `135ed222b`，早于本次上游合并：新增 11 处字号硬编码，使计数由 `208` 增至 `219`；`settings.ts` 新增认证状态依赖，间接加载会访问 `window` 的语言初始化模块，而 `browserSource.test.ts` 未隔离该依赖，导致 5 项测试的公共初始化失败。

修复将这 11 处字号改用已有主题变量，保留默认字号及 `208` 的检查上限；窄屏标题由 `--app-text-3xl` 乘以 `1.1` 保持默认 `22px`。测试沿用 `sessionInputState.test.ts` 的认证状态替身（代替真实模块的固定测试实现），避免为纯状态测试加载浏览器初始化代码。验收标准为原 6 项测试及前端全量测试、类型检查、生产构建通过。

已验证：本地 Node.js `24.14.0` 下，`npm test` 共 2,071 项，2,070 项通过、0 项失败、1 项因浏览器环境条件跳过。字号计数恢复为 `208`，原 5 项状态测试全部通过；`npm run type-check`、`npm run build` 和 `git diff --check` 通过。构建仍有静态脚本及大分包提示。下一步是修复推送后核验 GitHub Actions（自动检查任务）；本地验证不替代上文真实环境与浏览器整套回归的验证。

## 2026-10-21 本地整合记录

本次目标为 `main`：将 Tencent/WeKnora `e0c28bef3562de7d2d78d6c1fa480a045468a40f` 的 7 个新增提交合并到 fork `551209e0e9b2852f9efc8966f45a7c6465aec196`，以本地合并提交保存。代码检查和隔离数据库验证通过；未推送，也未更新父仓库引用。下一步是部署前核查目标数据库和共享沙箱历史目录，以及真实接口。

### 行为与兼容性

| 范围 | 已实现的合并结果与依据 |
| --- | --- |
| 定时任务 | 固定由内置助手执行，移除任务的 `agent_id` 和执行智能体选择；`model_id` 保留。需要其他智能体时在任务指令中指定委派。原先依赖特定智能体配置的任务须重新审阅；迁移删除旧执行者字段，回退迁移不能恢复原选择。执行、权限和状态服务测试通过，规则见[自动化设计](../../docs/design/agent-automation.md)。 |
| 来源引用 | 模型临时引用改为 `kb://N`、`doc://N`、`chunk://N`、`url://N`；持久文本使用网页地址或 `weknora://` 对象链接。旧短引用仍可在声明的标识字段中解析；不将普通正文中的相似字样作为引用。模型上下文、聊天流水线和链接渲染测试通过。 |
| 共享沙箱 | 会话目录改为会话 ID 的 SHA-256（固定长度摘要）前 12 位；工具允许访问同一机器其他目录，文件工具仍保护附件输入目录。旧完整 ID 目录没有自动搬迁逻辑，已启用共享模式的环境须先核验目录与检查点。默认开关未变；细节和边界见[沙箱分配方式](../../website-docs/03-features/22-skills-sandbox.md#user-sandbox)。 |
| 冲突 | 解决六种语言、`markdownDomPurify.ts` 和 SQLite 结构测试共 8 个文件的冲突。翻译按键合入上游修改，保留 Vivibit 品牌与本地布局；安全过滤同时保留图片状态与对象链接属性；结构测试使用本地迁移编号。 |
| 本地定制 | 打包、镜像构建入口、空内置模型、登录页与用户菜单、持久化及 `host.docker.internal` 白名单文件与合并前逐字节一致。保留法务提示词、语言策略、重复失败工具调用拦截、流式正文显示及图片恢复；相关回归测试通过。模型适配代码未变，根模块测试包含脱敏及思考参数回归。 |

双方共同修改的文件已逐项审查。上游删除的 `frontend/src/views/automation/agents.ts` 只负责原执行者列表，随执行者选择一并移除；替代执行入口及行为由 `automation_test.go` 和 `automationInteractions.test.ts` 覆盖，没有删除本地定制测试。

### 迁移与验证

已知仓库历史上界为 PostgreSQL（服务端关系数据库）`154`、SQLite（轻量文件数据库）`59`；上一发布版 `vos-hybrag-v0.1.74`（`4f0f5e1dc`）的上界分别为 `141`、`47`。未读取部署数据库状态，目标环境上界仍为 `待验证`。逐字节比对确认合并前及上一发布版的所有迁移路径、内容不变。新迁移仅追加：PostgreSQL `000133_automation_assistant` → `000155_automation_assistant`，SQLite `000050_automation_assistant` → `000060_automation_assistant`。

扩展 `TestSandboxAutomationMigrationUpgrade`，使用临时 SQLite 文件和独立 PostgreSQL 17 容器中的临时数据库。SQLite 覆盖空库及 `47`、`55`、`59` 升级；PostgreSQL 覆盖空库及 `141`、`150`、`154` 升级。历史基线使用已核验内容不变的迁移前缀，不修改业务库或迁移版本。

**已验证：** 两套数据库分别达到 `60/dirty=false`、`155/dirty=false`；`agent_id` 已移除，无需该字段即可插入任务；模型字段类型与非空约束、既有沙箱索引和主键约束符合预期。既有会话、任务指令、运行关联、审批状态和回答保留；从合并前版本升级时指定的模型保留，较早版本使用空模型默认值。再次升级无待执行项。本轮没有补偿迁移。

其余检查通过：两套迁移文件配对、`git diff --check` 与暂存区检查、根模块 `go test ./...`、前端 `npm run type-check`、`npm run build-only`、`npm test`（2,005 项通过，1 项因浏览器环境条件跳过）。模拟登录后的浏览器跳转覆盖知识库、智能体、设置、新对话、其他会话及定时任务会话，无未处理的 Vue（前端界面框架）或 JavaScript 错误。构建保留静态脚本及大分包提示。**待验证：** 真实接口写入链路、真实模型与共享沙箱运行、生产数据库备份副本升级；模拟接口和隔离迁移不能证明这些路径通过。

## 2026-10-20 增量整合记录

本次目标为 `main`：将 Tencent/WeKnora `968ca9fb1b7dab6f96dcbf3ee7e4c330abca253f` 的 5 个新增提交整合到 fork `09bae5069fad01a941bcd56a496d3dd943b91595`。本地代码与隔离数据库验证通过；真实服务接口、部署和生产数据库升级仍为 `待验证`。本次只保存本地合并，不推送、不更新父仓库引用。下一步是审阅合并，再按部署范围验证真实接口及数据库备份副本。

已合入定时任务的模型选择、执行身份、取消与调度限制、通知收件箱，以及共享沙箱的分配和回收。共享沙箱仍默认关闭；启用前须完成所有实例升级，再配置 `WEKNORA_SANDBOX_USER_ALLOCATION=enabled` 和 Redis（多实例共享状态服务），最后切换具体沙箱配置。行为、目录边界和回退条件见[沙箱分配方式](../../website-docs/03-features/22-skills-sandbox.md#user-sandbox)。

### 本地兼容与冲突处理

| 范围 | 合并结果与验证依据 |
| --- | --- |
| 聊天连接 | `streame.ts` 保留本地按会话管理连接及 `enable_memory`，加入上游 `resume_pending_id`；流式连接与审批恢复测试通过 |
| 会话和服务注册 | `session.go`、`container.go`、`types/session.go` 同时保留法务工作区和上游沙箱所有者认证来源、分配及清理逻辑；会话、法务、权限和容器测试通过 |
| 翻译 | 解决 `en-US.ts`、`ja-JP.ts`、`zh-TW.ts` 冲突；六种语言按键合入新增内容，保留本地文案与布局，前端类型和单元测试通过 |
| 迁移测试 | `migration_sqlite_versioned_schema_test.go` 同时保留本地表结构断言和上游沙箱断言，并更新编号；新增双数据库空库及升级回归测试 |
| 部署、品牌及模型 | `ictrek.app/` 打包、构建入口、空内置模型、Vivibit 品牌、个人模型权限、V-Scheduler（统一模型调度服务）接入、持久化和 `host.docker.internal` 白名单保留；这些配置及模型适配代码未被本轮上游修改 |

本轮上游没有删除或重命名原有模块及测试。双方共同修改的配置、接口文档、会话、权限、注册入口和页面已作语义检查；模型脱敏与思考参数实现保持原样，根模块测试包含其回归测试。

### 迁移编号与验证

已知仓库历史上界为 PostgreSQL（服务端关系数据库）`150`、SQLite（轻量文件数据库）`55`。上一发布标签 `vos-hybrag-v0.1.74` 对应 `4f0f5e1dc`，其上界分别为 `141`、`47`。逐文件比对确认合并前两套迁移的路径和内容未变；新迁移按依赖追加，未填补旧空号。未读取生产数据库状态；若目标环境版本超过本次已知上界，部署前须重新核验来源。

| 用途 | PostgreSQL 上游 → 本地 | SQLite 上游 → 本地 |
| --- | --- | --- |
| 会话所有者认证来源 | `000129` → `000151` | `000046` → `000056` |
| 共享沙箱分配与机器记录 | `000130` → `000152` | `000047` → `000057` |
| 定时任务执行时长 | `000131` → `000153` | `000048` → `000058` |
| 定时任务模型选择 | `000132` → `000154` | `000049` → `000059` |

`TestSandboxAutomationMigrationUpgrade` 使用临时 SQLite 文件和独立 PostgreSQL 17 容器中的临时数据库。验证路径为：空库安装、上一发布版历史迁移集升级，以及本次合并前历史迁移集升级。历史迁移集使用与原提交逐字节一致的文件前缀；不修改业务库或迁移版本。

已验证：SQLite 最终为 `59/dirty=false`，PostgreSQL 为 `154/dirty=false`；新增字段类型与非空约束、5 个沙箱索引、分配记录与机器记录的主键约束均符合预期。升级保留既有会话的所有者、标题和 `workspace_mode`；从 `55`／`150` 升级还保留任务指令、执行记录关联、审批状态和回答，新增模型默认空、执行时长默认 `0`。再次升级返回无待执行项。本轮无补偿迁移，不涉及历史漏项修复验证。

其余已验证项：两套迁移配对检查、`git diff --check`、根模块 `go test ./...`、前端 `npm run type-check`、`npm run build-only` 和 `npm test`（1,976 项通过、1 项因浏览器环境条件跳过）。模拟登录后的浏览器检查覆盖聊天、知识库、智能体、设置、新对话、其他会话及定时任务会话的跳转，定时任务会话显示只读提示，未见未处理的 Vue（前端界面框架）或 JavaScript 错误。模拟接口不证明实际接口与数据库写入链路通过；真实模型、共享沙箱运行和生产升级仍为 `待验证`。

## 2026-10-20 首轮本地整合记录

本次目标为 `main`：将 Tencent/WeKnora `eecfe4b3c8c34c94e9b559e5dd02187b4ae447cd` 整合到 fork `d28a44c4ad277dc4f43d669cc72e1c9e1f080050`，以本地合并提交保存。未推送，也未更新父仓库引用。下一步是审阅合并提交，获得推送授权后再同步远端和父仓库引用。

本地打包、空内置模型默认配置、VOS（应用运行平台）登录、个人模型权限和法务工作区保留。个人模型接口继续按记录归属校验；助手的模型写操作额外保留 `admin` 权限要求。聊天连接恢复使用本地按会话管理的连接，避免切换会话时取消其他会话；同一会话的新请求取代旧连接。视觉模型的逐次调用选项穿过本地脱敏和并发限制，继续阻止脱敏失败后的原文直发。归档解析结果的复用仍只允许内部导入调用。

数据库迁移编号保留已存在的 PostgreSQL（服务端关系数据库）`000001`–`000141` 和 SQLite（轻量文件数据库）`000001`–`000047` 历史。新增上游迁移按下表追加；已有数据库由原版本继续升级，不重跑旧迁移。

| 迁移用途 | PostgreSQL 上游 → 本地 | SQLite 上游 → 本地 |
| --- | --- | --- |
| 助手配置 | `000120` → `000142` | `000038` → `000048` |
| 会话种类 | `000121` → `000143` | `000039` → `000049` |
| 飞书重新同步标记 | `000122` → `000144` | `000040` → `000050` |
| 资源内容哈希索引 | `000123` → `000145` | 无新增迁移 |
| 智能体协作 | `000124` → `000146` | `000041` → `000051` |
| 清理空图片分块 | `000125` → `000147` | `000042` → `000052` |
| MCP（外部工具接入协议）工具风险 | `000126` → `000148` | `000043` → `000053` |
| 操作授权 | `000127` → `000149` | `000044` → `000054` |
| 定时任务 | `000128` → `000150` | `000045` → `000055` |

已验证：根模块、`client/` 和 `cli/` 的 `go test ./...`；两套迁移文件配对检查、SQLite 全量迁移、空 PostgreSQL 全量迁移及其回归测试；前端类型检查、生产构建和单元测试（1,939 项通过、1 项因浏览器环境条件跳过）；文档解析的 46 项测试；浏览器中的图片显示、VOS 语言跟随和 V-Scheduler（统一模型调度服务）模型选择。模拟登录后的聊天页、知识库、智能体、设置、新对话和其他会话跳转通过，未见未处理的 Vue（前端界面框架）或 JavaScript 错误。浏览器检查使用模拟接口；实际部署、真实模型及既有生产数据库升级待验证。

## remote 设置

在 WeKnora submodule 中保留两个 remote：

```bash
git remote -v
```

期望：

```text
origin    git@github.com:ictrektech/WeKnora.git
upstream  git@github.com:Tencent/WeKnora.git
```

如果缺少 upstream：

```bash
git remote add upstream git@github.com:Tencent/WeKnora.git
```

## 合并流程

```bash
cd apps/WeKnora
git status --short
git fetch upstream main --prune
git checkout main
fork_before=$(git rev-parse HEAD)
upstream_target=$(git rev-parse upstream/main)
```

无冲突的 merge 也必须做语义检查。先列出 fork 与 upstream 自共同基线以来都修改过的文件，逐个审查：

```bash
base=$(git merge-base "$fork_before" "$upstream_target")
comm -12 \
  <(git diff --name-only "$base" "$fork_before" | sort) \
  <(git diff --name-only "$base" "$upstream_target" | sort)
```

冲突处理原则：

- 保留当前 VOS app 文档和配置：`ictrek.app/`；
- 保留本地品牌和链接定制；
- 保留空的 `config/builtin_models.yaml` 默认行为，不把某台机器的模型后端写进镜像；
- 保留 compose 中持久化配置，并确认基础 `docker-compose.yml` 的 `SSRF_WHITELIST_EXTRA` 仍包含 `host.docker.internal`；
- 上游功能代码尽量合入，不做无关重构。

## 本地兼容保留检查

重叠文件列表只是审查线索。上游可能删除旧模块和测试，或把逻辑迁到另一个文件；必须同时查看两侧自共同基线以来的改动，不能只检查合并冲突或固定目录。

```bash
git diff --name-status --find-renames "$base" "$fork_before"
git diff --name-status --find-renames "$base" "$upstream_target"
```

对上游删除、重命名或重构的区域，读取 fork 原有实现、调用方、配置和测试。合并后逐项记录“保留”“已迁移（新实现和测试位置）”或“明确废弃（迁移说明）”。

删除定制测试前，必须找到覆盖相同输入和输出的替代测试；否则先补齐。缺少覆盖或尚未验证时，不得报告同步完成。

模型适配变化时，至少核对以下 ictrek 扩展：

| 已有配置 | 必须保留的请求行为 | 验证范围 |
| --- | --- | --- |
| `extra_config.thinking_control=think` | 显式开/关发送顶层 `think:true/false`；未指定时不发送 | 从模型配置解析到最终请求 JSON；流式和非流式 |
| `extra_config.thinking_control=reasoning_effort` | 旧布尔开/关发送顶层 `reasoning_effort:medium/none`；未指定时不发送 | 从模型配置解析到最终请求 JSON；流式和非流式 |

还需确认其他已支持值和未知非空值的原有回退行为未改变。表中是必须保留的契约，不代表某次合并或已部署镜像已验证通过。

历史案例：`f735f55c0` 删除旧思考适配及测试时遗漏了上述两项兼容，因此必须检查行为和测试的迁移去向。

完成本地兼容审查及下文[迁移历史与升级验证](#迁移历史与升级验证)的合并前历史审查后，合并已固定的上游提交：

```bash
git merge --no-ff --no-commit "$upstream_target"
```

`--no-ff` 保留 merge 拓扑，`--no-commit` 将提交留到验证通过后。

## 迁移历史与升级验证

每次同步都审查迁移差异，包括没有 Git 冲突、没有重复编号的情况。迁移工具按当前版本继续向前执行；`dirty=false` 只表示未标记为迁移中断，不证明历史字段和约束齐全。空库测试也不能证明存量升级成功。

### 历史审查与编号

合并前查看上游差异，合并并暂存后对照 `fork_before` 复核。禁用重命名识别，让旧路径的删除与新路径的新增都可见：

```bash
git diff --name-status --no-renames "$base" "$upstream_target" -- migrations/versioned/ migrations/sqlite/
git diff --cached --name-status --no-renames "$fork_before" -- migrations/versioned/ migrations/sqlite/
```

两套目录独立编号、独立验证，不能相互比较版本大小。每套目录分别执行以下规则：

1. 确认既有历史上界（不能被新迁移插入到其之前的最高编号）：取 `fork_before` 中的最高编号、受支持已发布版本的最高编号，以及已知目标环境的迁移版本三者的最大值。读取环境状态限于已授权范围；记录发布基线、版本及依据，不假定所有环境与本机相同。历史上界无法确认时，标记 `待验证`，不得宣称存量升级安全。
2. 已发布或可能已执行的 `.up.sql` 和 `.down.sql` 保持编号、路径和内容不变。上游修改、删除或重命名这些文件时保留 fork 原文件，把需要的结构或数据修正写入追加迁移。
3. 新进入 fork 部署历史的迁移必须高于该上界，按依赖顺序连续追加；禁止填补历史空号。未与已有文件重号但低于上界的迁移也必须追加。仅对尚未进入 fork 部署历史的上游文件使用 `git mv` 成对重编号；同步文件内注释、文档链接、测试期望和版本引用。
4. 上游变更已由 fork 等效实现时，先核对结构、数据语义及依赖，再记录映射和不重复执行的依据，不能机械重复搬运 SQL（数据库操作语句）。

目标数据库版本高于当前代码迁移集时，先确认其代码版本和额外迁移来源，不能只按本地最大编号分配新号或直接启动后端。

例如，既有历史上界为 `141`，新合入的 `122` 即使没有编号冲突，也必须追加到 `142` 或更高的可用编号。如果旧 `122` 已在 fork 历史中、但某个版本为 `150` 的库缺少其字段，则保留旧文件，在确认历史上界后追加更高编号的补偿迁移；不能改旧文件期待它重新执行。

补偿迁移应检查实际对象的类型、可空性、约束及数据。`ADD COLUMN IF NOT EXISTS` 只避免重名错误，不能修正已存在但类型错误的列。重复执行的安全性也应覆盖数据修正语句。

### 必需验证与完成条件

启动本地后端或提交合并前，先运行现有文件检查：

```bash
bash scripts/check-migration-files.sh migrations/versioned
bash scripts/check-migration-files.sh migrations/sqlite
git diff --check
```

这些命令只验证文件名、配对及差异格式，不验证历史不可变、低编号新增或数据库结构。失败时先修复，不得启动后端或提交合并。

涉及迁移变更时，对受影响的数据库类型完成以下隔离验证。使用独立数据库或备份副本，禁止在业务库上试验、清库、回退迁移或手动修改迁移版本；不得用 `force` 或自动恢复 dirty 状态掩盖失败。

| 验证路径 | 基线与验收条件 |
| --- | --- |
| 空库安装 | 使用合并后的完整迁移集从空库执行；检查最终 `version/dirty` 和受影响字段、类型、可空性、约束及索引。 |
| 存量升级 | 至少覆盖上一发布版本；其他受支持版本有不同结构或数据路径时补充覆盖。用对应版本原始迁移集建库并写入代表性数据，或恢复备份副本，再切换到新迁移集执行升级。检查数据内容和关联保留，以及预期的数据转换，不能只看行数。 |
| 历史漏项修复 | 在高版本但缺少目标对象的隔离副本上验证补偿迁移；同时覆盖对象已存在的正常库。补偿 SQL 重复执行后数据及结构仍符合预期，迁移工具再次升级应无待执行项。 |
| 功能验证 | 调用受影响的真实接口并核对数据库写入和错误日志。异步安装等流程分别记录请求受理、记录写入和最终任务状态，`202` 不等于安装完成。 |

记录基线提交或备份来源、初始与最终 `version/dirty`、新旧编号映射、结构及数据断言、接口结果和未覆盖环境。没有可用环境或基线时，明确列为 `待验证`；允许保存合并工作供审查，但不得报告迁移验证通过或同步验证完成。部署、重启和发布仍遵守原有授权范围。

**已实现：** `scripts/check-migration-files.sh` 的文件检查，以及 `.github/workflows/vos-migration-check.yml` 的空 PostgreSQL 全量迁移检查。**未实现：** 低编号新增、已发布迁移内容不可变和存量升级的专用自动检查；上述人工审查与隔离验证不能由现有工作流通过代替。

## 合并后检查

冲突处理并暂存后，先与保存的合并前提交比较，复核删除和迁移结果：

```bash
git diff --cached --name-status --find-renames "$fork_before"
```

未提交的合并不能用 `HEAD^1` 代替 `fork_before`；此时 `HEAD` 仍指向合并前提交。

重点查这些本地定制是否还在：

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  frontend config ictrek.app docker-compose.yml docker-compose.override.yml
```

前端源码或路由发生变化时，提交前必须完成类型检查；运行时代码、构建配置或依赖发生变化时，再执行构建。测试按受影响范围运行，全量测试交给大范围同步或 CI：

```bash
cd frontend
npm run type-check
cd ..
```

UI 或路由发生变化时，再登录验证聊天页到知识库、智能体、设置、新对话及其他会话的跳转，并确认控制台没有未处理的 `ReferenceError` 或 Vue 错误；运行时代码、构建配置或依赖发生变化时执行 `npm run build-only`。

模型适配变化时，迁移并运行上述请求契约的回归测试；当前相关包为：

```bash
go test ./internal/models/runtime ./internal/models/api/openaicompletions ./internal/models/chat
```

包路径变化时调整命令，保留契约覆盖并记录实际测试及结果。部署验证须在授权范围内检查正文非空、问题落库和推荐接口结果，不能只看 `HTTP 200` 或任务 `success`；未执行则记为待验证。

验证通过后再看状态并提交：

```bash
git status --short
git diff --check
git commit
```

如需构建镜像，按 [build-images.md](build-images.md) 走构建和飞书更新流程。部署和发布以 [../README.md](../README.md) 的 VOS app 流程为准；旧独立部署文档只在 [legacy](legacy/) 中备查。

## 提交顺序

先提交并推送 WeKnora submodule：

```bash
git add <changed-files>
git commit -m "..."
git push origin main
```

然后回到总仓库更新 submodule 指针：

```bash
cd ../..
git add apps/WeKnora
git commit -m "Update WeKnora"
git push origin main
```

---

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

Even a conflict-free merge needs a semantic review. List files changed by both
sides since their common base and review them one by one:

```bash
fork_before=$(git rev-parse main)
upstream_target=$(git rev-parse upstream/main)
base=$(git merge-base "$fork_before" "$upstream_target")
comm -12 \
  <(git diff --name-only "$base" "$fork_before" | sort) \
  <(git diff --name-only "$base" "$upstream_target" | sort)
```

Follow the [local compatibility preservation checklist](#本地兼容保留检查)
before merging; it defines the protected contracts and replacement-test requirements.

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
ictrek.app/*
```

## Merge

Merge upstream into the ictrek fork branch:

```bash
git checkout main
git merge --no-ff --no-commit "$upstream_target"
```

Keep the merge topology and defer the commit until verification passes.

## Conflict Handling

For conflicts, keep upstream functional fixes unless they overwrite deliberate
ictrek deployment or branding decisions.

Preserve these ictrek decisions unless the operator explicitly changes them:

- `ictrek.app/` keeps the current VOS app package, release, model, build, and sync notes.
- `ictrek.app/docs/legacy/` is reference-only. Do not prefer legacy standalone deployment content over current VOS app behavior.
- `build_image.sh` remains the ictrek image build and Feishu update entrypoint.
- `config/builtin_models.yaml` ships no deployment-specific model rows by
  default.
- prompt templates use `Vivibit` as the brand name; assistant-role descriptions follow the response language. See [chat language rules](USERGUIDE.md#11-快速问答).
- login and user menu links point to ictrek/Vivibit destinations.
- local persistence behavior stays documented and stable. The base
  `docker-compose.yml` must keep `host.docker.internal` in
  `SSRF_WHITELIST_EXTRA` so model rows that call host-mapped vLLM/Ollama
  backends do not fail when a deployment intentionally omits
  `docker-compose.override.yml`.

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

For every sync, follow [migration history and upgrade validation](#迁移历史与升级验证),
even without conflicts. That section is the source of truth for numbering,
immutable history, compensation migrations, and required isolated tests.
New incoming migrations must be above the existing history ceiling for their
database type; an unused lower number can still be skipped by existing databases.
The file checker and empty-database workflow do not prove upgrade safety.

After resolving conflicts, stage and inspect the result. Do not commit before
the verification gates pass:

```bash
git status --short
```

## Verification

Compare the staged merge with `fork_before` and complete the
[post-merge checks](#合并后检查) before committing.

After the merge, re-check the ictrek invariants:

```bash
rg -n "Vivibit|www.vivibit.com|ictrektech/WeKnora|host.docker.internal|builtin_models: \\[\\]" \
  config frontend/src/views/auth frontend/src/components/UserMenu.vue \
  docker-compose.yml docker-compose.override.yml ictrek.app
```

For code-level verification, use the build path documented in
`build-images.md`. Build and deployment should run on the selected remote host,
not locally, unless the task explicitly asks for a local check.

If frontend source or routing changed, run before committing:

```bash
cd frontend
npm run type-check
cd ..
```

Run `npm run build-only` when runtime code, build configuration, or dependencies
changed. Run affected tests; reserve the full suite for broad syncs or CI. For
UI or routing changes, log in and smoke-test navigation from chat to knowledge
bases, agents, settings, new chat, and another session. The browser console
must have no unhandled `ReferenceError` or Vue error.

The VOS migration workflow runs only for matching `pull_request` events or
after a push to remote `main`; a local merge or local commit does not trigger
GitHub Actions. It runs the versioned migration checker and a full PostgreSQL
empty-database migration, but it does not validate SQLite migrations or the
current local development database. Always run the local checker commands
before starting a local backend and before committing the merge.

After verification passes, commit the merge:

```bash
git status --short --branch
git diff --check
git commit
```

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
