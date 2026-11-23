# V-Note Office/PDF conversion

## Use and configure

In V-Note on VOS, select a notebook and choose **Import Office / PDF**. Choose one DOCX, XLSX, PPTX, or PDF. Use **Import history** to follow conversion, retry a failure, or open the note. The result includes editable text and the original attachment. Conversion uses the same VOS account associated in HybRAG and uses the tenant's parser rules and OCR configuration. The built-in PDF reader uses the default vision model for scanned pages. Configured cloud engines may receive document data. The effective input limit is the lower of 100 MiB and HybRAG's deployment file limit.

## Internal HTTP contract

The VOS package enables the route through `HYBRAG_VNOTE_DOCUMENT_DIR` on its persistent runtime volume. The default internal HybRAG address used by V-Note is `http://hybrag-app:8080`. These routes support the VOS integration.

- `PUT /api/v1/vnote/document-imports/{id}` accepts the raw file body, a current VOS `Authorization: Bearer <proof>`, `X-Document-Token` containing a random 64-character capability, and `X-Document-Filename` containing a percent-encoded basename. The job ID is a UUID. A successful submission or replay for the same owner and capability returns `202`. Invalid input returns `400`, a missing or invalid proof `401`, denied workspace access `403`, a disabled route `404`, a binding or ID conflict `409`, a size violation `413`, an unsupported extension `415`, and unavailable storage `503`.
- `GET /api/v1/vnote/document-imports/{id}` requires `Authorization: Document <capability>`. While enqueued it returns `202` with boolean `processing` and optional integer `done` and `total` scanned pages. A completed job returns `200` with `markdown` and an `images` array. Each image has `ref`, `filename`, and `mimeType` strings. A conversion failure returns `422` with `code` set to `document_parse_failed`, `document_ocr_unavailable`, `document_ocr_failed`, or `document_empty`. An unknown or expired job or a wrong capability returns `404`.
- `GET /api/v1/vnote/document-imports/{id}/images/{image}` uses the same credential. A zero-based image index from the result returns `200 application/octet-stream`. Invalid or unavailable images return `404`.
- `DELETE /api/v1/vnote/document-imports/{id}` uses the same credential and acknowledges a terminal job. It returns `204` after cleanup, `409` while conversion is active, `404` for unavailable jobs or wrong capabilities, or `503` if storage cleanup fails.

Failed V-Note imports keep the original upload for seven days for retry. HybRAG removes uncollected terminal jobs after seven days. Only completed V-Note notes retain the original as a normal attachment. Deploy one HybRAG app instance for each VOS installation and preserve its runtime volume on upgrade. The V-Note migration adds import task records. Existing note, attachment, and knowledge-base records retain their schemas.

# V-Note Office／PDF 转换

## 使用与配置

在 VOS 的 V-Note 网页端选择笔记本，点击 **导入 Office／PDF** 并选取一个 DOCX、XLSX、PPTX 或 PDF。可在 **导入记录** 查看进度、重试或打开生成的笔记。笔记保留可编辑正文和可下载的原件。转换使用 HybRAG 中关联的同一 VOS 账号，以及租户的解析器规则和 OCR 配置。内置 PDF 解析器的扫描页使用租户默认视觉模型。启用了云端引擎时，相关文档数据可能发往该服务。单个文件限制取 100 MiB 和 HybRAG 部署限额中的较小值。

## 内部 HTTP 契约

VOS 安装包通过持久运行目录上的 `HYBRAG_VNOTE_DOCUMENT_DIR` 启用转换。V-Note 调用的 HybRAG 默认内部地址为 `http://hybrag-app:8080`。以下接口供 VOS 内部联动使用。

- `PUT /api/v1/vnote/document-imports/{id}` 的请求体为原文件。提交使用当前 VOS 用户的 `Authorization: Bearer <proof>`、随机 64 字符能力凭据头 `X-Document-Token` 和百分号编码的原文件名头 `X-Document-Filename`。任务 ID 为 UUID。首次成功提交或同一所有者以同一凭据重放时返回 `202`。输入无效返回 `400`，证明无效返回 `401`，工作区不符合要求返回 `403`，路由未启用返回 `404`，账号关联或任务 ID 冲突返回 `409`，文件超限返回 `413`，扩展名不受支持返回 `415`，存储不可用返回 `503`。
- `GET /api/v1/vnote/document-imports/{id}` 使用 `Authorization: Document <capability>`。排队或转换中返回 `202`，包含布尔值 `processing` 及可选的已识别和总扫描页整数 `done`、`total`。成功返回 `200`，包含 `markdown` 字符串及 `images` 数组。每项包含 `ref`、`filename` 和 `mimeType` 字符串。转换失败返回 `422`，`code` 为 `document_parse_failed`、`document_ocr_unavailable`、`document_ocr_failed` 或 `document_empty`。未知任务、过期任务或错误凭据返回 `404`。
- `GET /api/v1/vnote/document-imports/{id}/images/{image}` 使用同一凭据。按结果中从零开始的索引下载图片，成功返回 `200 application/octet-stream`，无效或不可用时返回 `404`。
- `DELETE /api/v1/vnote/document-imports/{id}` 使用同一凭据确认终态任务。清理成功返回 `204`，任务尚在运行返回 `409`，任务不可用或凭据错误返回 `404`，存储清理失败返回 `503`。

V-Note 导入失败时保留原文件七天用于重试。HybRAG 七天后清理未领取的终态任务。导入完成后，V-Note 笔记正常保留原件附件。每台 VOS 运行一个 HybRAG 应用实例，升级时保留运行目录卷。V-Note 数据库迁移新增导入任务表。
