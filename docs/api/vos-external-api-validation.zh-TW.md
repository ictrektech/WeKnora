# VOS 外部 API 驗證與客戶需求覆蓋

[返回目錄](./README.md)

本文記錄 HybRAG 作為 VOS App 安裝後，外部系統如何在不使用 admin 帳號的情況下存取 RAG 能力，並對客戶《VOS API 整合需求與產品架構討論稿 V1.0》的要求做覆蓋說明。

驗證環境：

- VOS 主機：`192.168.1.229`
- HybRAG 版本：`0.1.45`
- HybRAG API：`http://192.168.1.229:29081/api/v1`
- HybRAG 前端：`http://192.168.1.229:29080`
- VOS 登入介面範例：`http://192.168.1.229:8105`
- 測試身分：VOS 一般使用者

`29080` 與 `29081` 是 HybRAG VOS App 模板預設暴露的連接埠。`8105` 只是 229 測試環境中用於命令列登入 VOS 的平台後端連接埠範例，不屬於 HybRAG App 對外連接埠；正式整合應優先使用 VOS/OAuth 登入流程取得 access token。

本文中的 token、API Key 與密碼均使用佔位符。真實憑證不要寫入程式碼倉庫、日誌或交付文件。

## 結論

HybRAG 目前支援兩條外部呼叫路徑：

1. 外部系統先完成 VOS 登入或 OAuth 授權，拿到一般使用者的 VOS access token，然後以 `Authorization: Bearer <VOS_ACCESS_TOKEN>` 直接呼叫 HybRAG API。HybRAG 會驗證該 VOS token，並依照映射後的 HybRAG 使用者權限執行。
2. 一般使用者在 HybRAG「API 整合」中建立自己的個人 API Key，外部系統再以 `X-API-Key: <API_KEY>` 呼叫 HybRAG API。個人 API Key 只能使用受限能力與綁定的知識庫範圍。

外部系統不需要長期持有 admin 帳號。一般使用者也不需要請 admin 代發個人 API Key。

## 路徑一：VOS 登入後直接呼叫 HybRAG

### 1. 取得 VOS access token

命令列驗證可使用 VOS 登入 API。以下 `8105` 僅為 229 測試環境範例；正式產品整合建議使用 VOS/OAuth 授權碼流程取得 access token。

```bash
VOS_BASE="http://192.168.1.229:8105"
VOS_USER="<一般使用者名稱>"
VOS_PASS="<一般使用者密碼>"

VOS_TOKEN="$(curl -sS -X POST "$VOS_BASE/v1000/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$VOS_USER\",\"password\":\"$VOS_PASS\"}" \
  | jq -r '.data.access_token')"
```

正式外部應用若已透過 VOS OAuth 流程取得 access token，可直接使用該 token，不需要再呼叫使用者名稱與密碼登入。

### 2. 使用 VOS token 呼叫 HybRAG

```bash
BASE="http://192.168.1.229:29081/api/v1"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

列出目前使用者可見的知識庫：

```bash
curl -sS "$BASE/knowledge-bases?creator=all&page=1&page_size=20" \
  -H "Authorization: Bearer $VOS_TOKEN" | jq
```

建立對話會話：

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部 API 測試"}' \
  | jq -r '.data.id')"
```

指定知識庫發起 RAG 問答：

```bash
KB_ID="<knowledge_base_id>"

curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"請用一句話概括這份知識庫的重點\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

無會話檢索：

```bash
curl -sS -X POST "$BASE/knowledge-search" \
  -H "Authorization: Bearer $VOS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"測試知識庫重點\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq
```

## 路徑二：直接使用個人 API Key 呼叫 HybRAG

一般使用者先在 HybRAG「設定 -> API 整合」建立個人 API Key，並綁定允許存取的知識庫。外部服務端保存該 Key 後即可長期呼叫。

```bash
BASE="http://192.168.1.229:29081/api/v1"
API_KEY="<一般使用者個人 API Key>"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY" | jq
```

建立對話會話：

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部 API Key 測試"}' \
  | jq -r '.data.id')"
```

指定知識庫檢索：

```bash
KB_ID="<knowledge_base_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"測試知識庫重點\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }" | jq
```

如需限定到某一份或幾份文件，使用 `knowledge_ids`。這裡的 `knowledge_id` 就是業務語意裡的文件 ID：上傳檔案、匯入 URL 或建立手動知識後，回應裡的 `data.id` 即為該文件 ID；也可以從知識庫文件列表 API 返回的每筆記錄 `id` 取得。

```bash
KNOWLEDGE_ID="<knowledge_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"只檢索這份文件裡的內容\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"]
  }" | jq
```

指定知識庫問答：

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"請用一句話概括這份知識庫的重點\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## 229 實測結果

| 測試項 | 結果 | 說明 |
| --- | --- | --- |
| VOS 一般使用者登入 | 通過 | `POST /v1000/user/login` 返回 access token |
| VOS token 呼叫 HybRAG 目前使用者 | 通過 | `GET /auth/me` 返回映射後的 HybRAG 使用者 |
| VOS token 列出知識庫 | 通過 | 返回目前一般使用者可見知識庫 |
| VOS token 建立會話 | 通過 | `POST /sessions` 返回會話 ID |
| VOS token 知識檢索 | 通過 | `POST /knowledge-search` 返回 `200` |
| VOS token RAG 問答 | 通過 | `POST /knowledge-chat/:session_id` 返回 SSE，並出現完成事件 |
| 個人 API Key 目前使用者 | 通過 | `GET /auth/me` 返回 Key 綁定使用者 |
| 個人 API Key 建立會話 | 通過 | `POST /sessions` 返回會話 ID |
| 個人 API Key 知識檢索 | 通過 | `POST /knowledge-search` 返回 `200` |
| 個人 API Key RAG 問答 | 通過 | `POST /knowledge-chat/:session_id` 返回 SSE，並出現完成事件 |
| 一般使用者建立高權限 API Key | 已拒絕 | 請求 `manage_members` 返回 `403` |

本次測試知識庫沒有命中文件分塊，因此檢索結果筆數為 `0`，但介面認證、知識庫範圍校驗、會話建立與 SSE 生成鏈路均已通過。若要驗證 citation/source 的內容結構，需要使用包含已解析文件且能命中查詢的知識庫。

## 客戶需求覆蓋

| 需求 | 目前覆蓋情況 | 說明 |
| --- | --- | --- |
| 一般使用者外部存取自己的 RAG | 已覆蓋 | 支援 VOS token 與個人 API Key 兩種方式 |
| 不長期使用 admin 帳號 | 已覆蓋 | 一般使用者可自行建立個人 API Key；VOS token 也繼承一般使用者身分 |
| 指定 Knowledge Base / KB ID | 已覆蓋 | `knowledge_base_ids` 支援一個或多個知識庫 ID |
| 指定 Document / Knowledge ID | 應用側已有 API | 請求欄位為 `knowledge_ids`；文件 ID 對應 HybRAG 的 `knowledge_id` |
| Conversation / Session ID | 已覆蓋 | `POST /sessions` 建立，`/knowledge-chat/:session_id` 復用 |
| Authentication 方法 | 已覆蓋 | `X-API-Key` 與 `Authorization: Bearer <VOS_ACCESS_TOKEN>` |
| RAG Query / Chat API | 已覆蓋 | `/knowledge-chat/:session_id`，SSE 輸出 |
| Retrieval API | 已覆蓋 | `/knowledge-search`，返回檢索分塊列表 |
| Streaming | 已覆蓋 | RAG Chat 使用 SSE |
| Citation / Sources | API 支援，目前樣本未命中 | SSE `references` 事件與檢索結果包含來源欄位；本次測試 KB 查詢未命中文件，未覆蓋真實 citation 內容 |
| Knowledge Base CRUD | 應用側已有 API | 見 `knowledge-base.md`；本次鏈路測試未建立/刪除真實業務 KB，避免污染使用者資料 |
| Document ingest / parse / re-index | 應用側已有 API | 上傳、重新解析、批次重新解析、取消解析見 `knowledge.md` |
| KB ACL / 分享 | 應用側已有部分能力 | 組織分享、viewer/editor 權限見 `organization.md`；不等同於 NAS ACL |
| API Key create/list/update/delete | 已覆蓋主要鏈路 | 建立由 UI 完成，本次驗證了 Key 呼叫與高權限拒絕；API 支援列表、更新範圍/過期時間、刪除 |
| API Key scope | 已覆蓋 | 個人 Key 必須綁定可存取知識庫，且只允許 `retrieve`、`chat`、`ingest` |
| API Key revoke | 應用側支援 | 刪除 API Key 即撤銷 |
| API Key rotate | 透過重建實現 | 目前沒有獨立 rotate endpoint；做法是建立新 Key，切換呼叫方後刪除舊 Key |
| API Key expiry | 應用側支援 | 建立/更新 API Key 可傳 `expires_at_unix` |
| NAS Share / Folder / ACL | HybRAG 應用側不覆蓋 | 屬於 VOS/NAS 平台 API；HybRAG 只負責已發布到知識庫後的檢索與問答權限 |
| NAS -> RAG Publish / Unpublish | 部分覆蓋 | HybRAG 支援文件上傳、刪除、重新解析；從 NAS 資料夾選擇並發布到 KB 的完整 Portal 工作流需要 VOS/NAS API 配合 |
| User / Group / Role / Permission API | HybRAG 應用側不覆蓋 | 屬於 VOS 身分與權限平台；HybRAG 可繼承 VOS token 映射後的使用者身分 |
| Model Hub list / status | 部分覆蓋 | HybRAG 可管理自身模型設定；Model Hub 運行狀態屬於 Model Hub 應用 API |
| Health | 部分覆蓋 | HybRAG 有系統能力與服務狀態 API；VOS/Model Hub 全域健康狀態需對應平台 API |
| Audit log | 部分覆蓋 | HybRAG 有系統稽核日誌頁面/API；VOS/NAS/Model Hub 全域稽核需要平台側支援 |

## 整合建議

- 企業 Portal 或 imShare Server 後端優先使用個人 API Key 路徑：實作簡單、適合服務端長期保存，且可以按知識庫和能力限制權限。
- 需要嚴格繼承目前 VOS 登入使用者時，使用 VOS OAuth / 登入取得的 access token，直接作為 `Authorization: Bearer` 呼叫 HybRAG。
- RK3288 等終端設備不要保存 admin、平台級 API Key 或 VOS 使用者密碼。建議由業務後端保存受限個人 API Key，終端只持有設備憑證。
- NAS 原始檔案權限與 RAG 知識庫權限要分開設計：NAS 檔案可讀不代表自動進入 RAG；進入 RAG 前應有 Publish / Approval 流程。
- 客戶要求的 VOS User/Group/Role、NAS ACL、全域 audit/health 屬於 VOS 平台能力，HybRAG 可以消費身分並承載知識庫 API，但不能單獨替代 VOS 平台 API。
