# WeKnora 并发和队列配置

本文说明 ictrek 部署模板里的并发配置。实际部署时，把应用侧变量写到目标机 `.env`；模型服务侧变量写到对应模型容器的 env。

## 三层控制

| 层级 | 作用 | 主要变量 |
| --- | --- | --- |
| Asynq 后台任务池 | 控制后台任务 worker 总数，以及不同任务队列的调度权重。 | `WEKNORA_ASYNQ_CONCURRENCY`、`WEKNORA_ASYNQ_QUEUE_*` |
| 后台 LLM 限流 | 防止 Graph、Wiki、自动问题生成、摘要生成、多模态 VLM 把主 QA 模型并发吃满。 | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`、`WEKNORA_CHAT_RESERVED_CONCURRENCY`、`WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` |
| 模型服务容量 | 控制 Ollama、vLLM 或其他 OpenAI-compatible 服务实际能同时处理多少请求。 | `OLLAMA_NUM_PARALLEL`、`OLLAMA_CONTEXT_LENGTH`、`VLLM_MAX_NUM_SEQS`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE` |

队列权重不是硬性的模型并发预留。真正给聊天保留模型槽位的是后台 LLM 限流。`WEKNORA_CHAT_RESERVED_CONCURRENCY` 是 WeKnora 应用侧限制，不是 vLLM/Ollama 自带的硬隔离；后台 LLM 调用必须经过代码里的 `acquireBackgroundLLMSlot` 才会被限制。

## 机器资源评估流程

给一台新机器定模型、上下文、模型并发和聊天预留时，按下面顺序做，不要只按显存大小或 `max-num-seqs` 猜。

1. 先定在线体验目标。明确是否必须跑 VLM/Graph/Wiki、是否需要 16k 以上上下文、是否要在文档入库时还能稳定聊天。聊天必须最高优先级时，先预留 `2-3` 个主 QA 槽；多人同时使用再继续提高。
2. 选候选模型。优先用目标硬件已经验证能稳定启动的量化模型；同等效果下先选更小模型或更低显存量化。模型启动后显存不能长期贴近上限，至少留出 KV cache、embedding、数据库和系统余量。
3. 定上下文。上下文越大，KV cache 越多，满长并发越低。QA 模型需要超过 16k 上下文时，不要设成正好 `16384`；Orin NX 16G 起步用 `18000`。如果聊天或 Graph 变慢，优先把后台并发降到 1，再考虑换小模型或降低上下文。
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
```

如果 `background_llm_slots < 1`，说明模型/上下文/显存组合不足以同时跑后台增强和聊天，应降低上下文、换小模型，或关闭/降低 Graph、Wiki、VLM 后台任务。

6. 定 Embedding 并发。Embedding 模型最好独立服务。vLLM embedding 场景下，`CONCURRENCY_POOL_SIZE` 是文档 embedding 应用侧上限；如果希望聊天检索保留 2-3 个槽，就让 `CONCURRENCY_POOL_SIZE` 低于 embedding 服务侧总并发。Ollama 场景下优先分成 QA/VLM 容器和 embedding 容器。

7. 用线上状态回验。纯 Ollama 方案先看：

```bash
docker exec <ollama-qa-container> ollama ps
docker logs --tail 80 <ollama-qa-container>
docker exec <ollama-embedding-container> ollama ps
```

如果聊天请求已经开始排队，先降低 `WEKNORA_ASYNQ_CONCURRENCY`、Graph/Wiki 并发或 `CONCURRENCY_POOL_SIZE`，不要先提高 `OLLAMA_NUM_PARALLEL`。`OLLAMA_NUM_PARALLEL` 提高后显存、KV cache 和上下文一起涨，容易直接 OOM。

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
| `ollama-qa` | 聊天模型、VLM/图片理解模型 | `KnowledgeQA`、`VLLM` 使用 `source=remote`，`base_url=http://ollama-qa:11535/v1` | `OLLAMA_CONTEXT_LENGTH=18000`、`OLLAMA_QA_NUM_PARALLEL=3` 起步，`WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3`，`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` |
| `ollama-embedding` | embedding 模型，例如 `bge-m3:latest` | `Embedding` 使用 `source=remote`，`base_url=http://ollama-embedding:11535/v1` | `OLLAMA_EMBEDDING_NUM_PARALLEL=4` 起步，`CONCURRENCY_POOL_SIZE=1` |

只有一个 Ollama 容器时，把 `CONCURRENCY_POOL_SIZE` 降到 `1`，Graph/Wiki 默认低并发，接受文档入库和聊天可能互相排队。单容器只是简化部署，不是稳定生产配置。

`WEKNORA_REPARSE_WAIT_URLS` 在纯 Ollama 方案中应写 OpenAI-compatible gateway 的 `/v1/models`：

```env
WEKNORA_REPARSE_WAIT_URLS=http://ollama-qa:11535/v1/models,http://ollama-embedding:11535/v1/models
```

这样 app 启动钩子和部署脚本都会等 QA/VLM 与 embedding 服务 ready，再提交失败/未完成文档重解析。

## 主 QA/LLM 并发

对话、Graph 抽取、Wiki 生成、自动问题生成、文档摘要、多模态 VLM 可能共用同一个主 QA/LLM 模型。部署时按模型服务真实容量配置：

```dotenv
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=1
WEKNORA_WIKI_INGEST_MAP_PARALLEL=1
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=1
```

`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 应该对齐主 QA 模型服务的真实在线并发。Ollama 场景下通常和 QA Ollama 容器的 `OLLAMA_NUM_PARALLEL` 保持一致；vLLM 场景下通常和 `VLLM_MAX_NUM_SEQS` 保持一致。

后台 LLM 可用槽位近似为：

```text
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

如果两个值都大于 0，且 `main <= reserved`，WeKnora 仍会保留 1 个后台槽位，避免 Graph/Wiki/Question/Multimodal 完全不执行。如果任意一个值为空或为 `0`，后台 LLM 限流不会启用。

`WEKNORA_GRAPH_LLM_CONCURRENCY` 限制单文档 Graph 抽取中的 LLM 并发，并且代码会把它限制到 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY/2` 以内。

Wiki map/reduce 并发先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库未设置时，使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` 和 `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL` 作为部署级默认值；env 也为空时才回退代码默认值。小机器建议 env 默认设为 `1` 或 `2`，个别大知识库再通过 KB 配置提高。

## 单文档任务和队列权重

文档解析、Graph、Wiki、自动问题生成、摘要等后台任务都走 Asynq。队列权重通过 env 读取：

```dotenv
WEKNORA_ASYNQ_CONCURRENCY=2
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_PARSE=5
WEKNORA_ASYNQ_QUEUE_DEFAULT=4
WEKNORA_ASYNQ_QUEUE_LOW=1
WEKNORA_ASYNQ_QUEUE_MULTIMODAL=3
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=2
WEKNORA_REPARSE_INCOMPLETE_ON_START=true
WEKNORA_REPARSE_WAIT_URLS=
WEKNORA_REPARSE_READY_WAIT_SECONDS=300
```

`WEKNORA_ASYNQ_CONCURRENCY` 是后台 worker 总并发，必须小于等于 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY`。如果设得更高，Graph/Wiki/Question 等后台任务会先占住 worker 并阻塞在模型限流上，新的文字解析仍然排队。

Asynq 使用严格优先级：`critical` > `parse` > `default` > `multimodal` > `question` > `graph` / `low`。`WEKNORA_ASYNQ_QUEUE_*` 是优先级权重，不是每队列并发上限。`parse` 队列承载文档解析和批量重解析，默认高于 default/multimodal/graph/question；多模态 VLM 队列默认权重为 3，排在文本解析之后、图谱和问题生成之前。

小机器上不要把 Graph、Question、Multimodal 队列权重调太高。聊天请求本身不走这些后台队列，但后台任务仍可能竞争同一个 LLM 或 Embedding 模型服务。`WEKNORA_REPARSE_INCOMPLETE_ON_START=true` 会在服务启动时先等待 `WEKNORA_REPARSE_WAIT_URLS` 中的模型服务 ready，再把 failed/pending/processing 的文档重新入队；`finalizing` 只有在 `processed_at is null` 时才会整篇重跑。已经完成文字解析和向量入库、只是停在 VLM/Graph/Wiki 后台增强的 `finalizing` 文档不会重复 docreader、分块和 embedding。

启动扫描还会按知识库当前配置清理已关闭功能的后台任务：如果知识库关闭多模态识别，会删除/取消未完成的 VLM/OCR 多模态任务；如果关闭知识图谱，会删除/取消未完成的 Graph 抽取任务。重新打开多模态识别时，app 会检查已经完成文字解析、但最新 attempt 的 `multimodal` 阶段为 `skipped`、`cancelled` 或 `failed` 的文档，并从文本 chunk 里的图片链接补发 `image:multimodal` 任务，不重跑全文解析。

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
| `ollama-qa` | KnowledgeQA 和 VLM | `OLLAMA_CONTEXT_LENGTH=18000`、`OLLAMA_QA_NUM_PARALLEL=3` | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3`、`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` |
| `ollama-embedding` | bge-m3 embedding | `OLLAMA_EMBEDDING_NUM_PARALLEL=4` | `CONCURRENCY_POOL_SIZE=1`、`BATCH_EMBED_SIZE=4` |

这样聊天/VLM 至少保留 2 个槽位，文档 embedding 只消耗 `ollama-embedding` 容器。Orin NX 16G 统一内存不要先追高 QA 并发；机器稳定、内存和等待队列都有余量时，再逐步提高 QA 并发。

分离容器时，模型行使用 OpenAI-compatible gateway：

```text
KnowledgeQA  source=remote  base_url=http://ollama-qa:11535/v1
VLLM         source=remote  base_url=http://ollama-qa:11535/v1
Embedding    source=remote  base_url=http://ollama-embedding:11535/v1  dimension=1024
```

只有单 Ollama 容器时，才把三类模型都建成 `source=local` 并统一使用 `OLLAMA_BASE_URL`。单实例降级值：

```dotenv
OLLAMA_CONTEXT_LENGTH=18000
OLLAMA_NUM_PARALLEL=3
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=3
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
CONCURRENCY_POOL_SIZE=1
BATCH_EMBED_SIZE=4
```

## 推荐值

| 机器类型 | QA 服务并发 | 聊天保留 | Graph | Embedding 并发 | 说明 |
| --- | ---: | ---: | ---: | ---: | --- |
| Orin NX / L4T 分离 Ollama | 3 | 2 | 1 | 1 | 首选。QA/VLM 与 embedding 分容器，QA 上下文用 `18000` 起步，`WEKNORA_ASYNQ_CONCURRENCY=1`。 |
| 通用 4 并发主机 | 4 | 2 | 1-2 | 2-4 | 后台 worker 不超过 2，优先降低 Wiki 和 embedding。 |
| 9B vLLM 主机 | 按 `VLLM_MAX_NUM_SEQS` | 2-3 | 1-2 | 按 embedding 后端容量 | QA/Graph/Wiki/Question 共用主 QA 模型时，worker 不超过剩余后台槽。 |

## 调参判断

| 现象 | 优先调整 |
| --- | --- |
| 文档入库时聊天变慢 | 增大 `WEKNORA_CHAT_RESERVED_CONCURRENCY`，并把 `WEKNORA_ASYNQ_CONCURRENCY` 降到 `主模型并发 - 聊天预留` 以内。 |
| Graph 或 Wiki 很慢，但聊天正常 | 只有在模型服务还有余量时，才提高 `WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` 或知识库级 wiki map/reduce 并发。 |
| 卡在 embedding 阶段 | 先检查 embedding 服务是否 ready，再对比 `WEKNORA_ASYNQ_CONCURRENCY`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE`、Ollama `OLLAMA_NUM_PARALLEL`。 |
| Ollama 单实例聊天被文档入库拖慢 | 改成 QA/VLM 和 embedding 两个 Ollama 容器；单实例只能做 best-effort。 |
| GPU 显存接近打满 | 先降低模型服务侧并发、上下文长度或显存占用率，再把应用侧并发同步降下来。 |

## 现场确认

在目标机上看运行中的容器，不要只看 env 文件：

```bash
docker inspect <app-container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(WEKNORA_MAIN_QA_MODEL_CONCURRENCY|WEKNORA_CHAT_RESERVED_CONCURRENCY|WEKNORA_GRAPH_LLM_CONCURRENCY|WEKNORA_WIKI_INGEST_MAP_PARALLEL|WEKNORA_WIKI_INGEST_REDUCE_PARALLEL|CONCURRENCY_POOL_SIZE|BATCH_EMBED_SIZE)='

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

如果 `waiting > 0` 长时间存在，先降低后台并发或 `CONCURRENCY_POOL_SIZE`，不要只提高模型服务并发。

修改后要同步 env、compose 里的模型服务参数和模型行配置。只改 app env 时，重新执行对应 compose `up -d app` 即可让 app 读取新值。
