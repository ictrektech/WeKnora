UPDATE archive_documents
SET extraction_status = 'failed'
WHERE extraction_status = 'canceled'
  AND error_message = '用户已取消解析';
