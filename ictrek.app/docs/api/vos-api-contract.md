# VOS API 契约、密钥和生命周期

## V-Note 同步模型与拒绝重试

`POST /api/v1/vnote/import-note` 仅供联动协议使用，仍要求已绑定的当前用户证明与
单次源票据 `{ "ticket": "<单次票据>" }`。新建同步库优先使用工作区默认 Embedding，
其次部署默认；均未设置时仅允许唯一启用的 YAML 托管内置 Embedding。多个候选或
仅有未选中的自定义模型时，先配置默认模型。已有知识库继续使用原模型。

仅在当前请求尚未暂存或排入索引时，缺少可用模型返回 HTTP 422：
`{"code":"vnote_embedding_unavailable","accepted":false,"error":"Configure an active default embedding model, then retry"}`。
`code` 是固定拒绝码，`accepted` 固定为 `false`，`error` 是说明字符串。
源端可在修复模型配置后重试。其他 422、超时或响应丢失不保证任务未接收，不能直接
清除待确认任务；旧版不确定手动运行应先保存相同设置、等控制版本送达，再立即同步。
仍在执行的模型任务继续占用槽位。知识库响应中的 `managed_source="vnote"` 为服务端
计算的只读来源标记，普通库省略该字段；托管库无需摘要模型即可打开文档视图。

## V-Note 首次账号关联

旧账号在「设置 → 用户信息 → 关联 VOS 账号」确认当前 HybRAG 账号归属后，再返回
V-Note「个人中心 → 集成 → HybRAG」重试。此确认不开始同步、不开启后台委托、不读取
笔记正文，也不搬移知识库。新建账号通过严格 VOS 用户校验后，在创建事务内自动关联；
已有同名账号不会自动关联。

客户端调用 `POST /auth/vos-identity`（相对于下文 API base），请求为
`Content-Type: application/json`、`Authorization: Bearer <当前 HybRAG 用户 JWT>`，
正文 `{"access_token":"<新的 VOS 用户 access token>"}`。唯一正文参数 `access_token`
必填，字符串长度 1–16384；请求体最多 32768 字节。两个凭据缺一不可；应用服务 Token、
停用用户及无法核验的 VOS 证明均拒绝。目标 HybRAG 用户由 JWT 决定，不接受客户端指定
用户 ID、用户名或空间 ID。账号和 VOS subject 已经是同一关联时重复提交成功；任一端
已有不同关联时不覆盖。

成功 HTTP 200：`{"success":true,"data":{"bound":true}}`。响应禁止缓存
（`Cache-Control: no-store`）。HTTP 400 表示缺失/非法正文，401 表示无效 HybRAG 会话或
VOS 用户证明，403 表示部署禁用 VOS 认证；409 的 `code: "identity_conflict"` 表示已有
冲突关联，需要管理员核查。存储故障返回 500，身份绑定服务不可用返回 503。
400/401/403/500 沿用下文通用错误结构；409/503 返回 `success:false` 和错误说明。
恢复会话或服务后可安全重试；冲突不通过重试覆盖。不接受 API Key 或后台委托替代本次
用户确认，也不在前端持久保存 VOS 证明。

## 入口与完整字段

“设置 → API 文档”保留原有完整章节库，“完整字段 / Full contract”提供可搜索的原生操作参数、请求、返回和共享结构。生产 GIN_MODE=release 会禁用 /swagger，因此文档打包版本匹配的 schema，不要求管理员开启开发模式。下方目录覆盖该原生规范声明的全部操作；新引入的路由及有意开放的工具/provider JSON 仍以实际路由与所属协议为准，不伪造统一固定结构。

VOS API 通常为 `https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1`。独立映射存在时默认 API `http://<HOST>:29081/api/v1`、前端29080，安装参数可改变或取消映射。后端原生监听8080不是普遍对外地址，以 API 集成页显示的实际地址为准，不使用开发 SSH 别名作为接口域名。

## 用户身份和权限范围

外部调用可携带 `X-API-Key: $HYBRAG_KEY`，或经授权获得的 VOS/OAuth 用户 `Authorization: Bearer $VOS_ACCESS_TOKEN`。应用核验/映射用户后执行成员角色、capability和知识库范围限制。OAuth 不支持 password grant，使用既有授权码/Fastpath，或获授权的 VOS 登录接口进行命令行验证。密码、令牌及返回的 Key 不得写日志。

个人 Key 属于用户，不代表全工作区数据。普通用户可选择 retrieve/chat/ingest 并绑定允许的知识库；个人空间 Owner 不自动拥有全工作区或平台权限。CanAccessAllTenants=true 的平台管理员才可创建工作区级 Key。API Key 路由默认拒绝，只有明确策略放行才可访问；full_access工作区 Key 不是无限平台管理员。系统 Key、MCP endpoint token 具有独立范围和管理入口。HybRAG Key 不等于 Scheduler 推理 Key 或厂商 Key；统一配置模型不取消 HybRAG 自己的知识库和会话权限。

## API Key 创建、修改和撤销

使用当前有权限的用户会话/VOS Bearer 管理。id为工作区数字 ID，key_id为 Key 记录数字 ID，均不是秘密值，不复制其他租户的 ID。

| 方法/路径 | 请求 | 成功 |
| --- | --- | --- |
| GET `/tenants/{id}/api-keys` | 当前用户及工作区权限 | 200 `success:true,data:KeyRecord[]` |
| POST `/tenants/{id}/api-keys` | JSON `name:string,full_access:boolean,knowledge_base_ids:string[],capabilities:string[],expires_at_unix?:integer` | 201 `success:true,data:KeyRecord & {token:string}` |
| PUT `/tenants/{id}/api-keys/{key_id}` | 同创建的完整配置字段 | 200 `success:true,data:KeyRecord` |
| DELETE `/tenants/{id}/api-keys/{key_id}` | 无 body | 200 `success:true`，不是204 |

KeyRecord字段为 `id:integer,scope_type:string,owner_user_id?:string,name:string,api_key:string,full_access:boolean,knowledge_base_ids:string[],capabilities:string[],last_used_at?:date-time,expires_at?:date-time,created_at:date-time`。创建在 data.token 和 data.api_key 返回生成的秘密；当前有权限的列表和修改响应也可能解密返回已保存的 api_key，**本应用不是只显示一次秘密的接口**。缺少保留秘密的历史记录可能为空，应保护所有管理响应；静态加密不代表可以记录返回秘密。

创建要求 name非空、capability有效，个人数据能力还要求非空且有权限的KB范围。expires_at_unix为Unix秒，创建时必须在未来，返回时间使用序列化date-time。PUT是完整配置更新而非局部补丁，应提交所需名称、放行范围、能力等全部配置；省略布尔/数组会解码为false/空，可能改变权限。full_access=true会清空细粒度能力和KB范围且要求相应管理员权限。PUT不轮换秘密值；轮换应创建替代 Key、更新客户端、撤销旧 Key。DELETE撤销认证，不是卸载模型。

## 检索、入库和对话生命周期

只读对接创建限定KB的retrieve/chat Key，先调用/auth/me核对实际身份，再列出/knowledge-bases?creator=all可见知识库。knowledge_base_ids表示KB，knowledge_ids表示文档，不是document_ids。POST /sessions返回data.id，同一话题多轮复用会话，新话题新建。POST /knowledge-chat/{session_id}以JSON传query及需要的knowledge_base_ids/knowledge_ids/channel。SSE data为原生StreamResponse，response_type:"complete"结束当前轮次，HTTP200后仍可能流内报错，需检查流事件和最终状态。

/knowledge-search支持无会话检索，仍受授权范围限制。上传/URL导入/手工知识创建返回文档ID不表示解析索引完成，需轮询相应知识/任务状态。重复文件或URL可能409并返回已有Knowledge；超时后先核对任务，不盲目重复入库。停止、删除有不同权限和副作用；删除文档不能代替取消对话或卸载Scheduler权重，应使用对应流程的实际操作。

```bash
BASE='https://<VOS_HOST>:<VOS_PORT>/app/com.ictrek.hybrag/api/v1'
curl --fail-with-body -H "X-API-Key: $HYBRAG_KEY" "$BASE/auth/me"
curl --fail-with-body -H "X-API-Key: $HYBRAG_KEY" -H 'Content-Type: application/json' \
  -d '{"query":"查询相关记录","knowledge_base_ids":["<kb_id>"]}' "$BASE/knowledge-search"
```

## 返回、文件、分页和错误

多数JSON成功为success:true/data并带各操作自己的total/page/page_size；部分系统接口返回原始行/数组或code:0/msg/data。媒体、导出、SSE、MCP有独立Content-Type，不能强套统一JSON。审计/运行任务/wiki游标分页不同于page分页，按next_cursor/has_more继续。

业务错误常为success:false/error:{code,message,details}；认证/RBAC中间件可直接返回error字符串和可选code字符串，网关可能非JSON。403可能来自工作区、KB、路由策略、过期/撤销Key，不应通过换不相关模型“解决”。恢复依赖/资源后再有限退避重试，不重试永久校验和权限错误。查询与上传具有不同超时/幂等语义，写请求超时后先查询已有任务。

/r/:token和签名文件URL只授权编码的特定资源并可过期，不授予广泛API访问。MCP使用自己签发/可轮换的endpoint token及所选工具/KB范围。管理员诊断、本机对话框动作不默认向scoped Key开放，原章节库有各流程说明。中国大陆网络可能阻断国外API，即使本地发现成功也要分别验证上游连通、注册和用户权限。
