# WeKnora 并发和队列配置

本文说明 ictrek 部署模板里的并发配置。实际部署时，把应用侧变量写到目标机 `.env`；模型服务侧变量写到对应模型容器的 env。

## 四层控制

| 层级 | 作用 | 主要变量 |
| --- | --- | --- |
| Asynq 后台任务池 | 控制独立的 core、postprocess、enrichment、maintenance、shared、wiki worker 池。 | `WEKNORA_ASYNQ_*_CONCURRENCY`、`WEKNORA_WIKI_ASYNQ_CONCURRENCY` |
| 后台 LLM 限流 | 防止 Graph、Wiki、自动问题生成、摘要生成、多模态 VLM 把主 QA 模型并发吃满。 | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`、`WEKNORA_CHAT_RESERVED_CONCURRENCY`、`WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` |
| 模型服务容量 | 控制 Ollama、vLLM 或其他 OpenAI-compatible 服务实际能同时处理多少请求。 | `OLLAMA_NUM_PARALLEL`、`OLLAMA_CONTEXT_LENGTH`、`VLLM_MAX_NUM_SEQS`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE` |
| 单次请求上下文预算 | 控制最终答案合成和会话摘要单次 LLM 调用的输入/输出 token 分配。 | `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS`、`WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS`、`WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS`、`WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS` |

队列权重不是硬性的模型并发预留。真正给聊天保留模型槽位的是后台 LLM 限流。`WEKNORA_CHAT_RESERVED_CONCURRENCY` 是 WeKnora 应用侧限制，不是 vLLM/Ollama 自带的硬隔离；后台 LLM 调用必须经过代码里的 `acquireBackgroundLLMSlot` 才会被限制。

## 配置生效规则和最低基线

不要把这三层的数字相加或当成同一个并发值。它们分别限制「可领取任务数」「后台模型调用数」和「模型服务实际可执行请求数」；最终有效容量由其中最小的限制决定。

| 配置 | 控制对象 | 有效值与特殊行为 | 生效方式 |
| --- | --- | --- | --- |
| `WEKNORA_ASYNQ_CORE_CONCURRENCY`、`POSTPROCESS`、`ENRICHMENT`、`MAINTENANCE`、`WEKNORA_WIKI_ASYNQ_CONCURRENCY` | 五个专用 Asynq worker 池 | 每项都必须是 `>=1`。缺失、`0` 或负数会回退代码默认值，**不会关闭该池**。 | 系统设置保存值优先于 env；修改后重启 app。 |
| `WEKNORA_ASYNQ_SHARED_CONCURRENCY` | shared 弹性 worker 池 | `>=0`。设为 `0` 会关闭 shared 借用；负数或缺失回退默认值。普通 profile 默认 `1`，thor profile 默认 `0`。 | 系统设置保存值优先于 env；修改后重启 app。 |
| `WEKNORA_MAIN_QA_MODEL_CONCURRENCY` + `WEKNORA_CHAT_RESERVED_CONCURRENCY` | 主 QA 模型的后台 LLM 槽位 | 两者都大于 `0` 才启用后台预留。后台槽位通常是 `main - reserved`；若 `main <= reserved`，实现仍保留 1 个后台槽，避免任务永久不执行。任一值为空或 `0` 时不启用这道限流。 | env 在 app 进程首次使用时读取；修改后重启 app。 |
| `WEKNORA_MODEL_MAX_CONCURRENCY` 或模型行 `max_concurrency` | 同一模型 endpoint/served model 的后台 Chat、VLM、Embedding 调用 | `>0` 是每模型后台调用上限；`0` 或负数关闭全局默认闸门。模型行的显式 `max_concurrency` 可覆盖默认值。在线聊天不经过此闸门。 | 系统设置值会即时下发；修改 env 后重启 app。 |
| `WEKNORA_GRAPH_LLM_CONCURRENCY` | 单文档 Graph 抽取的 LLM 调用 | 取正整数，且会被主 QA 并发的一半上限约束。 | 修改 env 后重启 app。 |
| `WEKNORA_WIKI_INGEST_MAP_PARALLEL`、`WEKNORA_WIKI_INGEST_REDUCE_PARALLEL` | Wiki 生成阶段的 map/reduce LLM 调用 | 是部署级默认值；知识库的 `wiki_config.ingest_map_parallel` / `wiki_config.ingest_reduce_parallel` 优先。 | 修改 env 后重启 app；知识库配置在新任务开始时生效。 |
| `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS` | 最终答案合成的主模型上下文估算 | VOS 普通 profile 默认 `24000`，thor profile 默认 `65536`。应小于或等于模型实际上下文。 | 修改 env 后重启 app。 |
| `WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS` | 最终答案合成的上下文安全余量 | VOS 普通 profile 默认 `0`，用于把 `24000` 窗口按约 `16000` 输入和 `8000` 输出分配；thor profile 默认 `768`。系统会先预留安全余量，再预留最终输出 token。 | 修改 env 后重启 app。 |
| `WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS` | Agent 最终答案合成的最大输出 token | VOS 普通 profile 默认 `8000`，thor profile 默认 `24576`。如果配置过大，代码会按 `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS - WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS - 512` 自动夹紧，至少保留 512 token 输入预算。 | 修改 env 后重启 app。 |
| `WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS` | 普通知识库问答摘要/回答生成的最大输出 token | 正整数才生效，会覆盖 `conversation.summary.max_completion_tokens`。小模型或 Ollama 低并发机器不要盲目调大。 | 修改 env 后重启 app。 |
| `CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE` | 文档 embedding 请求数与单请求 batch 大小 | 前者限制应用侧 embedding 并发，后者增加单请求显存和吞吐。两者都不等于 Asynq worker 数。 | 修改 env 后重启 app。 |

最小可用基线是五个专用 worker 池均为 `1`，普通 profile 再给 shared `1`；thor profile 可把 shared 设为 `0` 关闭弹性借用。不要通过设置 core、postprocess、enrichment、maintenance 或 wiki 为 `0` 来停用 Graph、Wiki 或维护任务；应在对应知识库/功能配置中关闭功能或暂停任务，避免由于回退默认值而意外恢复执行。

## 机器资源评估流程

给一台新机器定模型、上下文、模型并发和聊天预留时，按下面顺序做，不要只按显存大小或 `max-num-seqs` 猜。

1. 先定在线体验目标。明确是否必须跑 VLM/Graph/Wiki、是否需要 16k 以上上下文、是否要在文档入库时还能稳定聊天。聊天必须最高优先级时，先预留 `2-3` 个主 QA 槽；多人同时使用再继续提高。
2. 选候选模型。优先用目标硬件已经验证能稳定启动的量化模型；同等效果下先选更小模型或更低显存量化。模型启动后显存不能长期贴近上限，至少留出 KV cache、embedding、数据库和系统余量。
3. 定上下文。上下文越大，KV cache 越多，满长并发越低。VOS 普通 profile 当前按 `24000` 上下文、`8000` 输出预算配置，输入预算约 `16000`；如果目标机器撑不住，先降低后台并发，再考虑换小模型或降低上下文。
4. 启动模型服务做实测。纯 Ollama 方案看 `OLLAMA_NUM_PARALLEL` 和 `OLLAMA_CONTEXT_LENGTH`；vLLM 方案看 `--max-model-len` 和 `--max-num-seqs`。二者本质都是“同一模型服务能同时接多少条请求”。vLLM 启动日志里的这行可以直接估算满长并发：

```text
Maximum concurrency for 18,000 tokens per request: 4.75x
```

这个数表示满长请求下的有效并发。Ollama 没有这条日志时，用 `OLLAMA_NUM_PARALLEL` 当服务侧并发上限，再通过实际聊天和解析压测回验。`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 不应高于 Ollama/vLLM 的真实可用并发，否则后台长任务会把聊天压住。

5. 定 WeKnora 应用侧并发。推荐公式：

```text
WEKNORA_MAIN_QA_MODEL_CONCURRENCY = min(OLLAMA_NUM_PARALLEL 或 VLLM_MAX_NUM_SEQS, 实测有效并发)
WEKNORA_CHAT_RESERVED_CONCURRENCY = 2-3
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS <= WEKNORA_CHAT_MODEL_CONTEXT_TOKENS - WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS - 512
```

如果 `background_llm_slots < 1`，说明模型/上下文/显存组合不足以同时跑后台增强和聊天，应降低上下文、换小模型，或关闭/降低 Graph、Wiki、VLM 后台任务。

最终答案输出预算也要跟上下文一起调。VOS 普通 profile 使用 `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS=24000`、`WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS=0`、`WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS=8000`，给合同审查等长答案保留 8k 输出，并留下约 16k 输入预算。配置超过可用窗口时，代码会自动夹紧，但不要依赖夹紧来掩盖模型上下文设置错误。

6. 定 Embedding 并发。Embedding 模型最好独立服务。vLLM embedding 场景下，`CONCURRENCY_POOL_SIZE` 是文档 embedding 应用侧上限；如果希望聊天检索保留 2-3 个槽，就让 `CONCURRENCY_POOL_SIZE` 低于 embedding 服务侧总并发。Ollama 场景下优先分成 QA/VLM 容器和 embedding 容器。

7. 用线上状态回验。纯 Ollama 方案先看：

```bash
docker exec <ollama-qa-container> ollama ps
docker logs --tail 80 <ollama-qa-container>
docker exec <ollama-embedding-container> ollama ps
```

如果聊天请求已经开始排队，先降低 enrichment、shared、Wiki worker，或降低 Graph/Wiki 并发和 `CONCURRENCY_POOL_SIZE`，不要先提高 `OLLAMA_NUM_PARALLEL`。`OLLAMA_NUM_PARALLEL` 提高后显存、KV cache 和上下文一起涨，容易直接 OOM。

vLLM 方案再看：

```bash
curl -sS http://127.0.0.1:<vllm-metrics-port>/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
docker logs --tail 50 <qwen-vllm-container> 2>&1 \
  | grep -E 'Running:|Waiting:|GPU KV cache usage'
```

如果没有聊天时主模型 `Running` 长期等于或高于 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY`，这是正常后台占用；如果聊天时持续排队，优先降低后台槽、Graph/Wiki/VLM 并发或上下文。不要用提高 Asynq worker 数解决模型排队。

## 纯 Ollama 部署注意事项

纯 Ollama 方案不要把所有模型塞进一个容器后再期待 WeKnora 能硬隔离资源。`OLLAMA_NUM_PARALLEL` 是整个 Ollama 实例的调度并发，无法区分聊天、图片理解和 embedding。

推荐拆成两个 Ollama 容器：

| 容器 | 模型 | WeKnora 模型配置 | 资源限制 |
| --- | --- | --- | --- |
| `ollama-qa` | 聊天模型、VLM/图片理解模型 | `KnowledgeQA`、`VLLM` 使用 `source=remote`，`base_url=http://ollama-qa:11535/v1` | `OLLAMA_CONTEXT_LENGTH=24000`、`OLLAMA_QA_NUM_PARALLEL=8` 起步，`WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8`，`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` |
| `ollama-embedding` | embedding 模型，例如 `bge-m3:latest` | `Embedding` 使用 `source=remote`，`base_url=http://ollama-embedding:11535/v1` | `OLLAMA_EMBEDDING_NUM_PARALLEL=4` 起步，`CONCURRENCY_POOL_SIZE=2` |

只有一个 Ollama 容器时，把 `CONCURRENCY_POOL_SIZE` 降到 `1`，Graph/Wiki 默认低并发，接受文档入库和聊天可能互相排队。单容器只是简化部署，不是稳定生产配置。

`WEKNORA_REPARSE_WAIT_URLS` 在纯 Ollama 方案中应写 OpenAI-compatible gateway 的 `/v1/models`：

```env
WEKNORA_REPARSE_WAIT_URLS=http://ollama-qa:11535/v1/models,http://ollama-embedding:11535/v1/models
```

这样 app 启动钩子和部署脚本都会等 QA/VLM 与 embedding 服务 ready，再提交失败/未完成文档重解析。

## 主 QA/LLM 并发

对话、Graph 抽取、Wiki 生成、自动问题生成、文档摘要、多模态 VLM 可能共用同一个主 QA/LLM 模型。部署时按模型服务真实容量配置：

```dotenv
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=1
WEKNORA_WIKI_INGEST_MAP_PARALLEL=1
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=1
WEKNORA_CHAT_MODEL_CONTEXT_TOKENS=24000
```

`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 应该对齐主 QA 模型服务的真实在线并发。Ollama 场景下通常和 QA Ollama 容器的 `OLLAMA_NUM_PARALLEL` 保持一致；vLLM 场景下通常和 `VLLM_MAX_NUM_SEQS` 保持一致。

后台 LLM 可用槽位近似为：

```text
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

如果两个值都大于 0，且 `main <= reserved`，WeKnora 仍会保留 1 个后台槽位，避免 Graph/Wiki/Question/Multimodal 完全不执行。如果任意一个值为空或为 `0`，后台 LLM 限流不会启用。

`WEKNORA_GRAPH_LLM_CONCURRENCY` 限制单文档 Graph 抽取中的 LLM 并发，并且代码会把它限制到 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY/2` 以内。

Wiki map/reduce 并发先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库未设置时，使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` 和 `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL` 作为部署级默认值；env 也为空时才回退代码默认值。小机器建议 env 默认设为 `1` 或 `2`，个别大知识库再通过 KB 配置提高。

最终答案合成不再无上限塞入全部工具结果。`WEKNORA_CHAT_MODEL_CONTEXT_TOKENS` 用来告诉应用主 QA 模型的实际上下文，应用会先预留 `WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS` 输出 tokens 和 `WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS` 安全余量，再计算输入预算；超过预算时裁掉较旧工具结果，保留用户问题、系统提示、最新工具结果和最终回答指令。`WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS` 默认是 `2048`，配置过大时会按 `WEKNORA_CHAT_MODEL_CONTEXT_TOKENS - WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS - 512` 自动夹紧，至少给输入上下文保留 512 token。上下文设得比模型真实能力大，会重新出现长检索结果挤爆最终答案的问题；设得太小，会减少最终答案可参考的旧工具结果。

## 单文档任务和 worker 池

文档解析、Graph、Wiki、自动问题生成、摘要等后台任务都走 Asynq。当前代码不再读取旧的总预算 `WEKNORA_ASYNQ_CONCURRENCY`，也不读取 `WEKNORA_ASYNQ_QUEUE_*`；它们可以从 `.env` 和 compose 中删除，继续保留不会改变调度结果。

```dotenv
WEKNORA_ASYNQ_CORE_CONCURRENCY=1
WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY=1
WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY=1
WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY=1
WEKNORA_ASYNQ_SHARED_CONCURRENCY=1
WEKNORA_WIKI_ASYNQ_CONCURRENCY=1
WEKNORA_MODEL_MAX_CONCURRENCY=6
WEKNORA_REPARSE_INCOMPLETE_ON_START=true
WEKNORA_REPARSE_WAIT_URLS=
WEKNORA_REPARSE_READY_WAIT_SECONDS=300
```

每个变量是单个 app 实例对该池的保证 worker 数，不能再用一个总数推导分配。普通 profile 六项均为 `1` 时，单个 app 实例的 worker 基线是 `1 + 1 + 1 + 1 + 1 + 1 = 6`；thor profile 按 LexAI thor 资源策略把 shared 设为 `0`，基线是 `4 + 2 + 2 + 1 + 0 + 4 = 13`。这是 worker 数，不是模型调用数。`shared` 只订阅 core 和 enrichment 队列，在这两个池有积压时提供弹性容量；maintenance 和 Wiki 不会借用 shared。小机器从专用池 `1`、shared `1` 开始。只有对应队列积压且下游服务有余量时，才单独提高该池；除 shared 外，其他池的 `0` 或负数都会回退默认值，不会禁用该池。

| worker 池 | 队列与任务 | 资源含义 |
| --- | --- | --- |
| core | `default`：文档解析、手动处理 | 文本解析、分块、向量化有独立保证；不会被 Graph/VLM 占满 worker。 |
| postprocess | `postprocess`：解析完成后的状态收敛和增强任务 fan-out | 轻量收尾不会排在长 DocReader 后面。 |
| enrichment | `summary`、`multimodal`、`graph`、`question` | 摘要、图片理解、知识图谱、自动问题等增强任务。 |
| maintenance | `sync`、`low`：数据源同步、批量删除、批量重解析、索引删除 | 批量和维护操作不会堵住新文档解析。`low` 是兼容旧 Redis 任务的物理队列名。 |
| shared | `default`、`summary`、`multimodal`、`graph`、`question` | core 与 enrichment 的弹性补充；不应在小机器上用它提高模型请求并发。 |
| wiki | `wiki`：Wiki ingest/finalize | `WEKNORA_WIKI_ASYNQ_CONCURRENCY` 单独控制，不与前三个池共享 worker。 |

worker 数量只决定可取走多少后台任务，不等于可以同时调用多少次模型。`WEKNORA_MODEL_MAX_CONCURRENCY` 是所有后台 Chat/VLM/Embedding 调用的每模型默认硬闸门，按模型 endpoint/served model 共享；如果 QA 与 VLM 指向同一 vLLM 或 gateway，它们会共用同一槽位。模型行自己的 `max_concurrency` 可覆盖该默认值。在线聊天不走后台 worker，也不经过这个后台闸门。

因此不要把任一 worker 池直接设成 `主模型并发 - 聊天预留`。core 中的文本解析和 embedding 可以继续工作；真正需要按主 QA 剩余容量收紧的是 `WEKNORA_MODEL_MAX_CONCURRENCY`、`WEKNORA_GRAPH_LLM_CONCURRENCY` 与 Wiki map/reduce。模型服务已经接近满载时，先把 enrichment、shared 和 Wiki worker 降低。

系统设置优先级是「系统管理页面保存值 > 容器环境变量 > 代码默认值」。`asynq.core_concurrency`、`asynq.postprocess_concurrency`、`asynq.enrichment_concurrency`、`asynq.maintenance_concurrency`、`asynq.shared_concurrency`、`asynq.wiki_concurrency` 修改后必须重启 app 才会重建 worker 池；`model.max_concurrency` 会即时下发到限流器，不需要重启。管理员可在系统设置的运行时队列页面查看六个池的实例数、容量、利用率、各队列积压，以及模型限流的 active/waiting/limit；这比只读 `.env` 更可信。若 UI 中曾保存过 worker 值，单独修改 `.env` 不会覆盖它；必须在 UI 改回目标值或删除对应系统设置后再重启。

`WEKNORA_REPARSE_INCOMPLETE_ON_START=true` 会在服务启动时先等待 `WEKNORA_REPARSE_WAIT_URLS` 中的模型服务 ready，再把 failed/pending/processing 的文档提交到 maintenance 池；`finalizing` 只有在 `processed_at is null` 时才会整篇重跑。已经完成文字解析和向量入库、只是停在 VLM/Graph/Wiki 后台增强的 `finalizing` 文档不会重复 docreader、分块和 embedding。

启动扫描先删除旧 attempt 和完全重复的 Asynq 任务，再按知识库当前配置清理已关闭功能的后台任务。重新打开多模态识别时，只有队列中不存在对应任务，app 才从文本 chunk 的图片链接补发 `image:multimodal`，避免同一文档重复占用 VLM。日志可搜索 `startup-task-reconcile`。

## Embedding 并发

文档向量化主要看这几个参数：

```dotenv
BATCH_EMBED_SIZE=4
CONCURRENCY_POOL_SIZE=4
```

`BATCH_EMBED_SIZE` 是单次 embedding 请求里打包的 chunk 数。

`CONCURRENCY_POOL_SIZE` 是应用侧文档 embedding 请求并发上限。它如果低于文档 worker 数，后台解析可能看起来卡在 embedding 阶段；它如果高于 embedding 服务容量，聊天检索和文档入库会同时排队。

如果 embedding 模型是单独 vLLM 服务，让 `CONCURRENCY_POOL_SIZE` 低于该服务的 `max-num-seqs`，给在线检索留余量。如果 embedding 用 Ollama，`OLLAMA_NUM_PARALLEL` 控制 Ollama 服务侧并发。

## Orin NX / L4T 纯 Ollama 推荐值

Orin NX 这类机器上，如果 Ollama 一个实例同时跑 QA/VLM 和 embedding，`OLLAMA_NUM_PARALLEL` 只能限制整个实例，不能分别给聊天和 embedding 保留槽位。推荐分成两个容器：

| 容器 | 用途 | Ollama 并发 | WeKnora 侧配置 |
| --- | --- | ---: | --- |
| `ollama-qa` | KnowledgeQA 和 VLM | `OLLAMA_CONTEXT_LENGTH=24000`、`OLLAMA_QA_NUM_PARALLEL=8` | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY=8`、`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` |
| `ollama-embedding` | bge-m3 embedding | `OLLAMA_EMBEDDING_NUM_PARALLEL=4` | `CONCURRENCY_POOL_SIZE=2`、`BATCH_EMBED_SIZE=4` |

这样聊天/VLM 至少保留 2 个槽位，文档 embedding 只消耗 `ollama-embedding` 容器。Orin NX 16G 统一内存不要先追高 QA 并发；机器稳定、内存和等待队列都有余量时，再逐步提高 QA 并发。

分离容器时，模型行使用 OpenAI-compatible gateway：

```text
KnowledgeQA  source=remote  base_url=http://ollama-qa:11535/v1
VLLM         source=remote  base_url=http://ollama-qa:11535/v1
Embedding    source=remote  base_url=http://ollama-embedding:11535/v1  dimension=1024
```

只有单 Ollama 容器时，才把三类模型都建成 `source=local` 并统一使用 `OLLAMA_BASE_URL`。单实例降级值应按机器实测下调，例如：

```dotenv
OLLAMA_CONTEXT_LENGTH=24000
OLLAMA_NUM_PARALLEL=4
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
CONCURRENCY_POOL_SIZE=2
BATCH_EMBED_SIZE=4
```

## 推荐值

| 机器类型 | QA 服务并发 | 聊天保留 | Graph | Embedding 并发 | 说明 |
| --- | ---: | ---: | ---: | ---: | --- |
| Orin NX / L4T 分离 Ollama | 8 | 2 | 1 | 2 | VOS 普通 profile 默认。QA/VLM 与 embedding 分容器，embedding Ollama 总槽位 4，文档 embedding 用 2，给聊天检索留 2。 |
| 通用 4 并发单 Ollama 主机 | 4 | 2 | 1 | 1-2 | 只有单 Ollama 容器或机器实测撑不住 8 路时使用；需要同步降低 `OLLAMA_NUM_PARALLEL` 和 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`。 |
| 9B vLLM 主机 | 按 `VLLM_MAX_NUM_SEQS` | 2-3 | 1-2 | 按 embedding 后端容量 | QA/Graph/Wiki/Question 共用主 QA 模型时，worker 不超过剩余后台槽。 |

## 调参判断

| 现象 | 优先调整 |
| --- | --- |
| 文档入库时聊天变慢 | 增大 `WEKNORA_CHAT_RESERVED_CONCURRENCY`，把 `WEKNORA_MODEL_MAX_CONCURRENCY`、Graph/Wiki 并发先降到主模型真实剩余容量；必要时再降低 enrichment/Wiki worker。不要把任一 worker 设为 `0`，它会回退默认值。 |
| Graph 或 Wiki 很慢，但聊天正常 | 只有在模型服务还有余量时，才提高 `WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` 或知识库级 wiki map/reduce 并发。 |
| 卡在 embedding 阶段 | 先检查 embedding 服务是否 ready，再对比 core/shared、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE`、Ollama `OLLAMA_NUM_PARALLEL`。 |
| Ollama 单实例聊天被文档入库拖慢 | 改成 QA/VLM 和 embedding 两个 Ollama 容器；单实例只能做 best-effort。 |
| GPU 显存接近打满 | 先降低模型服务侧并发、上下文长度或显存占用率，再把应用侧并发同步降下来。 |

## 管理界面运行检查

系统设置页显示的是单个 app 实例的 worker 配置；带「需重启」标识的 worker 值保存后，必须重启 app。系统设置保存值优先于 `.env`，因此部署后应在页面确认值是否与模板一致。

![系统资源设置](images/system-resource-settings.png)

任务队列页用于区分 worker 队列与模型限流：

![任务队列运行时](images/task-queue-runtime.png)

- 「运行中/容量」表示所有 app 实例合计的任务 worker 使用量。`2/2` 只表示两个 worker 都在处理任务，不能据此推断模型有两路请求。
- 「模型并发占用」显示后台调用的 active、waiting 和 limit。waiting 持续增长说明应用侧模型闸门或模型服务已成为瓶颈，应先降低 enrichment、shared、wiki、Graph 或 embedding 并发，不要直接提高 worker。
- 默认（文档解析）排队高时，依次检查 core worker、docreader、embedding 和数据库写入；图谱、摘要、Wiki 排队高但聊天正常时，保持低优先级处理即可。
- 六个 worker 池都必须使用正整数。系统设置或 env 中的 `0`、负数和空值会回退代码默认值，不会停用该池；要关闭某项能力，应关闭知识库对应功能或暂停任务。

面向管理员的操作说明与界面截图见 [用户指南的系统资源章节](../USERGUIDE.md#15-系统资源与任务队列)。

## 现场确认

在目标机上看运行中的容器，不要只看 env 文件：

```bash
docker inspect <app-container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(WEKNORA_MAIN_QA_MODEL_CONCURRENCY|WEKNORA_CHAT_RESERVED_CONCURRENCY|WEKNORA_ASYNQ_(CORE|POSTPROCESS|ENRICHMENT|MAINTENANCE|SHARED)_CONCURRENCY|WEKNORA_WIKI_ASYNQ_CONCURRENCY|WEKNORA_MODEL_MAX_CONCURRENCY|WEKNORA_GRAPH_LLM_CONCURRENCY|WEKNORA_WIKI_INGEST_MAP_PARALLEL|WEKNORA_WIKI_INGEST_REDUCE_PARALLEL|WEKNORA_CHAT_MODEL_CONTEXT_TOKENS|WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS|WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS|WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS|CONCURRENCY_POOL_SIZE|BATCH_EMBED_SIZE)='

docker inspect <ollama-qa-container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(OLLAMA_NUM_PARALLEL|OLLAMA_KEEP_ALIVE|OLLAMA_CONTEXT_LENGTH)='

curl -fsS http://127.0.0.1:<qa-gateway-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-gateway-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'

docker exec <ollama-qa-container> ollama ps
docker exec <ollama-embedding-container> ollama ps
```

vLLM 场景看 waiting：

```bash
curl -sS http://127.0.0.1:<vllm-metrics-port>/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
```

如果 `waiting > 0` 长时间存在，先降低 `WEKNORA_MODEL_MAX_CONCURRENCY`、enrichment/shared/Wiki worker 或 `CONCURRENCY_POOL_SIZE`，不要只提高模型服务并发。

修改后要同步系统设置、env、compose 里的模型服务参数和模型行配置。修改任一 `WEKNORA_ASYNQ_*_CONCURRENCY` 或 `WEKNORA_WIKI_ASYNQ_CONCURRENCY` 后，重新执行对应 compose `up -d app`；修改 `WEKNORA_MODEL_MAX_CONCURRENCY` 可在系统设置页即时生效。
