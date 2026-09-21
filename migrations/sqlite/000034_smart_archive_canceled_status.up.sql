-- Preserve the cancellation result for documents canceled before the
-- extraction status had a dedicated terminal value.
UPDATE archive_documents
SET extraction_status = 'canceled'
WHERE extraction_status = 'failed'
  AND error_message = '用户已取消解析';
