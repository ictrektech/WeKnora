import { fetchEventSource } from '@microsoft/fetch-event-source';
import { ref, onUnmounted } from 'vue';
import { generateRandomString } from '@/utils/index';
import i18n from '@/i18n';
import { getApiBaseUrl } from '@/utils/api-base';
import {
  sanitizeStreamRequestBody,
  type StreamRequestMeta,
} from '@/utils/chatRequestDebug';

type StreamChunkHandler = (data: any) => boolean | void

const output = ref('')              // 显示内容
const isStreaming = ref(false)      // 流状态
const isLoading = ref(false)        // 初始加载
const error = ref<string | null>(null)// 错误信息
const lastStreamRequest = ref<StreamRequestMeta | null>(null)
const controllers = new Map<string, AbortController>()
const streamSessions = new Map<string, string>()
const pendingChunks = new Map<string, any[]>()
const chunkHandlers = new Set<StreamChunkHandler>()

const dispatchChunk = (chunk: any) => {
  let handled = false
  chunkHandlers.forEach((handler) => {
    try {
      if (handler(chunk) === true) handled = true
    } catch (err) {
      console.error('[Stream] chunk handler failed:', err, chunk);
    }
  })
  if (handled) return
  const sessionId = String(chunk.__stream_session_id || chunk.session_id || '')
  if (!sessionId) return
  const chunks = pendingChunks.get(sessionId) || []
  chunks.push(chunk)
  pendingChunks.set(sessionId, chunks)
}

interface StreamOptions {
  // 请求方法 (默认POST)
  method?: 'GET' | 'POST'
  // 请求头
  headers?: Record<string, string>
  // 请求体自动序列化
  body?: Record<string, any>
  // 流式渲染间隔 (ms)
  chunkInterval?: number
}

export function useStream() {
  // 流式渲染缓冲
  let buffer: string[] = []
  let renderTimer: number | null = null

  // 启动流式请求
  const startStream = async (params: { session_id: any; query: any; knowledge_base_ids?: string[]; knowledge_ids?: string[]; tag_ids?: string[]; agent_enabled?: boolean; agent_id?: string; web_search_enabled?: boolean; enable_memory?: boolean; summary_model_id?: string; mcp_service_ids?: string[]; skill_names?: string[]; mentioned_items?: Array<{id: string; name: string; type: string; kb_type?: string; kb_id?: string; kb_name?: string; service_id?: string; skill_name?: string}>; images?: Array<{data: string}>; attachment_uploads?: Array<{data: string; file_name: string; file_size: number}>; attachment_ids?: string[]; suggestion_attribution?: { suggestion_set_id: string; question_id: string }; method: string; url: string; embed_token?: string; embed_session_sig?: string; embed_visitor_id?: string }) => {
    const streamKey = `${params.method}:${params.url}:${params.session_id}:${params.query}`
    controllers.get(streamKey)?.abort()
    const currentController = new AbortController()
    controllers.set(streamKey, currentController)
    streamSessions.set(streamKey, String(params.session_id))
    // 重置状态
    output.value = '';
    error.value = null;
    isStreaming.value = true;
    isLoading.value = true;

    // 获取API配置
    const apiUrl = getApiBaseUrl();
    
    const embedToken = params.embed_token;
    const token = embedToken || localStorage.getItem('weknora_token');
    if (!token) {
      error.value = i18n.global.t('error.tokenNotFound');
      stopStream();
      return;
    }

    // 跨空间访问请求头：只要 setSelectedTenant 写过激活空间，就附
    // X-Tenant-ID。早期版本会 short-circuit "selectedTenantId ===
    // defaultTenantId 时不附" 来减少 header 体积，但任何把 weknora_tenant
    // 写成激活空间的代码（OIDC 同步 / UserMenu loadUserInfo / router
    // hydrate）都会让两者相等，使得后续流式请求悄悄丢 header、落到
    // home 空间上，导致 SSE 接口返回 404。直接附即可——后端
    // IsTenantAccessible 也允许 header 指向自家空间。
    const selectedTenantId = localStorage.getItem('weknora_selected_tenant_id');
    const tenantIdHeader: string | null = selectedTenantId || null;

    // TTFB instrumentation: record the moment we kick off the request so
    // we can compare it with the first answer chunk we receive from the
    // server. This makes it possible to correlate the frontend-observed
    // latency with the backend "TTFB:first_answer_chunk" log line by
    // matching on X-Request-ID.
    const sentAt = performance.now();
    const requestID = generateRandomString(12);
    let firstAnswerLogged = false;

    try {
      let url =
        params.method == "POST"
          ? `${apiUrl}${params.url}/${params.session_id}`
          : `${apiUrl}${params.url}/${params.session_id}?message_id=${params.query}`;
      console.log(`[TTFB] request:start request_id=${requestID} url=${url} sent_at=${Date.now()}`);
      
      // Prepare POST body with required fields for agent-chat
      // knowledge_base_ids array and agent_enabled can update Session's SessionAgentConfig
      const postBody: any = { 
        query: params.query,
        agent_enabled: params.agent_enabled !== undefined ? params.agent_enabled : true
      };
      // Always include knowledge_base_ids for agent-chat (already validated above)
      if (params.knowledge_base_ids !== undefined && params.knowledge_base_ids.length > 0) {
        postBody.knowledge_base_ids = params.knowledge_base_ids;
      }
      // Include knowledge_ids if provided
      if (params.knowledge_ids !== undefined && params.knowledge_ids.length > 0) {
        postBody.knowledge_ids = params.knowledge_ids;
      }
      // Include agent_id if provided (backend resolves shared agent and tenant from share relation)
      if (params.agent_id) {
        postBody.agent_id = params.agent_id;
      }
      // Include web_search_enabled if provided
      if (params.web_search_enabled !== undefined) {
        postBody.web_search_enabled = params.web_search_enabled;
      }
      // Include enable_memory if provided
      if (params.enable_memory !== undefined) {
        postBody.enable_memory = params.enable_memory;
      }
      // Include summary_model_id if provided (for non-Agent mode)
      if (params.summary_model_id) {
        postBody.summary_model_id = params.summary_model_id;
      }
      // Include mcp_service_ids if provided (for Agent mode)
      if (params.mcp_service_ids !== undefined && params.mcp_service_ids.length > 0) {
        postBody.mcp_service_ids = params.mcp_service_ids;
      }
      if (params.skill_names !== undefined && params.skill_names.length > 0) {
        postBody.skill_names = params.skill_names;
      }
      if (params.tag_ids !== undefined && params.tag_ids.length > 0) {
        postBody.tag_ids = params.tag_ids;
      }
      // Include mentioned_items if provided (for displaying @mentions in chat)
      if (params.mentioned_items !== undefined && params.mentioned_items.length > 0) {
        postBody.mentioned_items = params.mentioned_items;
      }
      // Include images if provided (base64 data URIs for multimodal chat)
      if (params.images !== undefined && params.images.length > 0) {
        postBody.images = params.images;
      }
      // Include attachment_uploads if provided (documents, audio, etc.)
      if (params.attachment_uploads !== undefined && params.attachment_uploads.length > 0) {
        postBody.attachment_uploads = params.attachment_uploads;
      }
	  if (params.attachment_ids !== undefined && params.attachment_ids.length > 0) {
		postBody.attachment_ids = params.attachment_ids;
	  }
      if (params.suggestion_attribution) {
        postBody.suggestion_attribution = params.suggestion_attribution;
      }
      postBody.channel = embedToken ? "embed" : "web";

      lastStreamRequest.value = {
        requestId: requestID,
        url,
        method: params.method,
        body: params.method === 'POST' ? sanitizeStreamRequestBody(postBody) : null,
        sentAt: Date.now(),
      };
      
      await fetchEventSource(url, {
        method: params.method,
        headers: {
          "Content-Type": "application/json",
          "Authorization": embedToken ? `Embed ${embedToken}` : `Bearer ${token}`,
          "Accept-Language": i18n.global.locale?.value || localStorage.getItem('locale') || 'zh-CN',
          "X-Request-ID": requestID,
          ...(!embedToken && tenantIdHeader ? { "X-Tenant-ID": tenantIdHeader } : {}),
          ...(params.embed_session_sig ? { "X-Embed-Session": params.embed_session_sig } : {}),
          ...(params.embed_visitor_id ? { "X-Embed-Visitor": params.embed_visitor_id } : {}),
        },
        body:
          params.method == "POST"
            ? JSON.stringify(postBody)
            : null,
        signal: currentController.signal,
        openWhenHidden: true,

        onopen: async (res) => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          console.log(`[TTFB] response:headers request_id=${requestID} elapsed_ms=${(performance.now() - sentAt).toFixed(1)}`);
          isLoading.value = false;
        },

        onmessage: (ev) => {
          if (controllers.get(streamKey) !== currentController) return
          const parsed = JSON.parse(ev.data);
          parsed.__stream_session_id = params.session_id;
          parsed.__stream_message_id = params.query;
          // Log first answer chunk for end-to-end TTFB measurement.
          // Filter by event type so non-answer events (references, tool
          // calls, etc.) don't count as the "first token" arrival.
          if (!firstAnswerLogged && (parsed?.response_type === 'answer' || parsed?.type === 'answer')) {
            firstAnswerLogged = true;
            console.log(`[TTFB] response:first_answer request_id=${requestID} elapsed_ms=${(performance.now() - sentAt).toFixed(1)}`);
          }
          buffer.push(parsed); // 数据存入缓冲
          dispatchChunk(parsed);
        },

        onerror: (err) => {
          throw new Error(`${i18n.global.t('error.streamFailed')}: ${err}`);
        },

        onclose: () => {
          if (controllers.get(streamKey) !== currentController) return
          controllers.delete(streamKey)
          streamSessions.delete(streamKey)
          isStreaming.value = controllers.size > 0;
          isLoading.value = false;
        },
      });
    } catch (err) {
      if (controllers.get(streamKey) === currentController) {
        controllers.delete(streamKey)
        streamSessions.delete(streamKey)
        error.value = err instanceof Error ? err.message : String(err)
        currentController.abort()
        isStreaming.value = controllers.size > 0
        isLoading.value = false
      }
    }
  }

  // 注册块处理器
  const onChunk = (handler: StreamChunkHandler) => {
    chunkHandlers.add(handler)
    onUnmounted(() => chunkHandlers.delete(handler))
    return () => chunkHandlers.delete(handler)
  }


  // 停止流
  const stopStream = (sessionId?: string) => {
    if (sessionId) {
      const targetSessionId = String(sessionId)
      for (const [streamKey, controller] of controllers.entries()) {
        if (streamSessions.get(streamKey) !== targetSessionId) continue
        controller.abort()
        controllers.delete(streamKey)
        streamSessions.delete(streamKey)
      }
      pendingChunks.delete(targetSessionId)
    } else {
      controllers.forEach((controller) => controller.abort())
      controllers.clear()
      streamSessions.clear()
      pendingChunks.clear()
    }
    isStreaming.value = controllers.size > 0;
    isLoading.value = false;
  }

  const hasActiveStream = (sessionId?: string) => {
    if (!sessionId) return controllers.size > 0
    for (const activeSessionId of streamSessions.values()) {
      if (activeSessionId === String(sessionId)) return true
    }
    return false
  }

  const drainSessionChunks = (sessionId: string) => {
    const targetSessionId = String(sessionId)
    const chunks = pendingChunks.get(targetSessionId) || []
    pendingChunks.delete(targetSessionId)
    return chunks
  }

  return {
    output,          // 显示内容
    isStreaming,     // 是否在流式传输中
    isLoading,       // 初始连接状态
    error,
    lastStreamRequest,
    onChunk,
    startStream,     // 启动流
    stopStream,      // 手动停止
    hasActiveStream,
    drainSessionChunks
  }
}
