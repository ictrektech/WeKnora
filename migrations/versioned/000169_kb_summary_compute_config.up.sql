-- Migration 000169: per-knowledge-base compute-node text inference routing for document summaries.
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS summary_compute_config JSONB;
