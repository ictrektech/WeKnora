# 法律工作台

本文面向工作区管理员、法务助手使用者和部署维护者，说明法律工作台的入口开关、法务对话助手、数据保留和数据删除边界。合同审查的功能、状态和结果判读见 [合同审查说明](../../docs/contract-review.md)；智能档案的导入、队列和提醒见 [智能档案说明](smart-archive.md)。

## 快速结论

| 项目 | 当前状态 |
| --- | --- |
| 左侧一级入口“法律工作台” | 已实现；新建或未配置工作区默认关闭，可由 `ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED` 改为开启；已保存配置优先 |
| 页面级 drill-down 子导航“法务助手 / 合同审查 / 智能档案” | 已实现；进入 `/platform/legal-assistant`、`/platform/contract-review` 或 `/platform/smart-archive` 后替换平台侧栏内容 |
| 法务对话助手 | 已实现；复用现有知识库、临时附件、SSE、引用和 Agent 对话能力，使用独立的 `legal_assistant` 会话归属 |
| 设置入口 | 已实现；设置 → 发布集成 → 法律工作台 |
| 关闭后的数据行为 | 已实现；只隐藏入口并阻止法律页面和法务会话访问，不删除数据；提醒调度跳过已关闭租户 |
| 工作区数据删除 | 已实现；工作区 Owner 二次确认后清理合同审查、法务助手会话及其会话级数据，普通平台会话不受影响 |
| PostgreSQL / SQLite 迁移验证 | 待验证；法律开关沿用 `000106`/`000016`，法务会话归属使用 `000110`/`000021`，需在目标部署执行升级验证 |

## 为什么增加这个开关

合同审查已经有独立工作台，但原入口无法按工作区隐藏。工作区可能暂时不使用法律功能，却需要保留已有审查记录和合同原文，因此“是否显示和允许访问”必须与“是否删除数据”分开管理。

采用工作区级配置而不是用户级配置，是因为入口和访问范围属于工作区能力；关闭开关只改变访问状态，不改变合同审查记录、条款、风险结果或上传文件。删除数据单独放在危险操作区域，并由 Owner 明确确认。

## 使用方式

1. 进入 `/platform/settings?section=general`。
2. 在“发布集成”中选择“法律工作台”。
3. Admin+ 可以切换“启用法律工作台”。
4. 开启后，左侧出现“法律工作台”；点击后进入 `/platform/legal-assistant`，侧栏只显示返回平台导航、法律工作台、法务助手、合同审查和智能档案。
5. 法务助手支持多轮问答、知识库和临时文件选择、网页检索开关、引用/附件展示；它的 Agent 和输入栏选择单独保存，离开后不会覆盖普通对话的选择。
6. 关闭后，左侧入口隐藏；正在访问的法律页面会返回知识库列表，服务端后续法律页面、法务会话读取/发送和会话级附件请求返回 `403`。重新开启后，保留的数据可以继续访问，提醒调度会恢复有效事件。

侧栏 drill-down 在展开状态使用小号灰色二级文字，不复用平台一级菜单的高亮样式；折叠状态保留返回、合同审查和智能档案图标按钮。

## 通过 `.env` 设置默认状态

在部署目录的 `.env` 中配置：

```dotenv
ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED=false
```

- `false`、未配置或无法解析时，法律工作台默认关闭；设置为 `true` 时默认开启。
- 该变量只控制新建工作区，或数据库中尚未保存法律工作台配置的工作区。设置页保存的工作区级开关优先于 `.env`。
- 修改 `.env` 后需要重启后端容器或 Go 后端进程；不会自动修改已有工作区，也不会删除任何法律工作台数据。
- 迁移 `000106`/`000107` 和 SQLite `000016` 为已有部署保留了 `enabled: true` 的数据库兼容默认值。因此，已经被迁移回填为 `true` 的旧工作区不会因 `.env=false` 自动关闭；如需关闭，请在设置页切换开关。

## 数据删除

设置页的“删除全部数据”与启用开关独立。只有工作区 Owner 可以执行；确认提示会说明以下内容将永久删除且不可恢复：

- 当前工作区的合同审查记录；
- 条款、风险问题和分析结果；
- 合同审查上传的源文件及其资源绑定。
- 法务助手的 `legal_assistant` 会话、消息、输入建议和临时会话附件。

关闭启用开关不会触发删除。删除操作只匹配 `workspace_mode=legal_assistant` 的会话，不会删除普通平台会话；共享资源仍被其他对象引用时会保留，外部存储清理失败会返回错误以便重试。

## 接口与权限

API 前缀为 `/api/v1`，配置按当前工作区隔离。

| 方法 | 路径 | 权限 | 作用 |
| --- | --- | --- | --- |
| `GET` | `/tenants/kv/legal-workspace-config` | Viewer+ | 读取开关；旧数据缺少配置时按部署默认处理 |
| `PUT` | `/tenants/kv/legal-workspace-config` | Admin+ | 请求体为 `{ "enabled": true\|false }`；关闭不删除数据 |
| `POST` | `/legal-assistant/sessions` | Viewer+ | 创建带 `workspace_mode=legal_assistant` 的法务助手会话 |
| `DELETE` | `/legal-workspace-data` | Owner+ | 删除当前工作区的合同审查数据、法务助手会话级数据和不再共享的源文件 |

关闭后，`/contract-review-playbooks`、`/contract-reviews/**`、`/legal-assistant/sessions` 以及带法务会话 ID 的读取、问答、流式、附件和产物接口均不可访问；删除接口不受该访问开关影响，以便 Owner 在关闭后仍可执行明确的数据清理。

## 迁移与验证

- PostgreSQL 使用 `migrations/versioned/000106_legal_workspace_config.up.sql`。
- 法务会话归属使用 `migrations/versioned/000110_session_workspace_mode.up.sql`。
- 如果数据库的迁移记录已经到 `000106`，但缺少该列，后续 `migrations/versioned/000107_legal_workspace_config_repair.up.sql` 会幂等补齐字段。
- SQLite/Lite 使用 `migrations/sqlite/000016_legal_workspace_config.up.sql`。
- 法务会话归属使用 `migrations/sqlite/000021_session_workspace_mode.up.sql`。
- 数据库新字段默认 `enabled: true`，兼容升级前已有合同审查数据；应用创建工作区时使用 `ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED` 的默认值。
- 代码验证包括前端 `npm run type-check`、`npm run check-i18n`，以及后端法律配置、会话归属、路由、数据清理和 SQLite migration 测试。
- 发布到实际 VOS 环境前，应在备份数据库上验证迁移、关闭后数据仍存在、重新开启后记录可读，以及删除确认后的共享文件保留行为。
