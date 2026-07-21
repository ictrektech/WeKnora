-- Migration: 000073_hybrag_model_hub_alias_repair
-- Description: Repair HybRAG VOS model rows created by older packages that
-- pointed at the now-removed in-app Ollama service aliases. Model Hub owns the
-- Ollama runtimes in VOS, so QA/VLM must use model-hub-ollama-qa and embedding
-- must use model-hub-ollama-embedding through the 11535 gateway.
DO $$ BEGIN RAISE NOTICE '[Migration 000073] Repairing HybRAG Model Hub Ollama aliases...'; END $$;

UPDATE models
SET
    display_name = CASE
        WHEN id = 'hybrag-ollama-qwen35-2b-qa' THEN 'Model Hub Ollama QA (model-hub-ollama-qa)'
        WHEN id = 'hybrag-ollama-qwen35-2b-vlm' THEN 'Model Hub Ollama VLM (model-hub-ollama-qa)'
        WHEN id = 'hybrag-ollama-bge-m3-embedding' THEN 'Model Hub Ollama Embedding (model-hub-ollama-embedding)'
        ELSE display_name
    END,
    parameters = (
        CASE
            WHEN type IN ('KnowledgeQA', 'VLLM') THEN
                jsonb_set(parameters::jsonb, '{base_url}', to_jsonb('http://model-hub-ollama-qa:11535/v1'::text), true)
            WHEN type = 'Embedding' THEN
                jsonb_set(parameters::jsonb, '{base_url}', to_jsonb('http://model-hub-ollama-embedding:11535/v1'::text), true)
            ELSE parameters::jsonb
        END
    )::json,
    updated_at = NOW()
WHERE id IN (
        'hybrag-ollama-qwen35-2b-qa',
        'hybrag-ollama-qwen35-2b-vlm',
        'hybrag-ollama-bge-m3-embedding'
    )
  AND (
        parameters::text LIKE '%hybrag-ollama-qa%'
        OR parameters::text LIKE '%hybrag-ollama-embedding%'
        OR display_name LIKE 'HybRAG Ollama%'
    );

DO $$ BEGIN RAISE NOTICE '[Migration 000073] HybRAG Model Hub Ollama alias repair complete'; END $$;
