# VOS API 契约、密钥和生命周期

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
