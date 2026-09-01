# VOS 外部 API 接入檢查清單

[返回目錄](./README.md)

本文提供外部系統接入 HybRAG API 時的設定檢查、呼叫順序與權限邊界說明。內容適用於 HybRAG 作為 VOS App 安裝後的場景，也適用於透過宿主機映射連接埠直接存取 RAG API 的整合方式。

## 接入地址

HybRAG API 統一使用 `/api/v1` 前綴。VOS App 模板預設提供兩類存取方式：

- VOS 應用路徑：`/app/com.ictrek.hybrag/api/v1`
- 宿主機直連：`http://<VOS主機IP>:29081/api/v1`

前端頁面預設映射到 `http://<VOS主機IP>:29080`。如果安裝時調整過 `HYBRAG_FRONTEND_PORT` 或 `HYBRAG_API_PORT`，請以實際連接埠為準。

## 認證路徑

HybRAG 支援兩條外部認證路徑。

### 一般使用者個人 API Key

一般使用者登入 HybRAG 後，在「設定 -> API 整合」建立個人 API Key，並綁定允許存取的知識庫範圍。外部服務端保存該 Key 後，用 `X-API-Key` 呼叫 API。

個人 API Key 只代表建立它的使用者：

- 不需要 admin 代發。
- 不具備空間級 full access。
- 可用能力限制為知識檢索、知識問答與文件寫入等個人範圍能力。
- 存取範圍受建立時選擇的 `knowledge_base_ids` 限制。

### VOS/OAuth Bearer Token

外部應用也可以先透過 VOS 登入或 OAuth 授權流程取得一般使用者的 access token，再以 `Authorization: Bearer <VOS_ACCESS_TOKEN>` 呼叫 HybRAG。

HybRAG 會驗證該 VOS token，映射為對應 HybRAG 使用者，並依該使用者權限執行。此方式適合需要嚴格繼承 VOS 目前使用者身分與權限的整合。

## 呼叫順序

### 1. 確認呼叫主體

```bash
BASE="http://<VOS主機IP>:29081/api/v1"
API_KEY="<一般使用者個人 API Key>"

curl -sS "$BASE/auth/me" \
  -H "X-API-Key: $API_KEY"
```

使用 VOS token 時：

```bash
VOS_TOKEN="<一般使用者 VOS/OAuth access token>"

curl -sS "$BASE/auth/me" \
  -H "Authorization: Bearer $VOS_TOKEN"
```

### 2. 列出可存取知識庫

```bash
curl -sS "$BASE/knowledge-bases?creator=all&page=1&page_size=20" \
  -H "X-API-Key: $API_KEY"
```

返回的知識庫 `id` 用作後續請求裡的 `knowledge_base_ids`。

### 3. 建立會話

```bash
SESSION_ID="$(curl -sS -X POST "$BASE/sessions" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"外部系統 RAG 會話"}' \
  | jq -r '.data.id')"
```

`session_id` 用於多輪對話。新話題可以重新建立會話；同一話題可以復用同一個會話。

### 4. 無會話檢索

```bash
KB_ID="<knowledge_base_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"檢索關鍵詞\",
    \"knowledge_base_ids\": [\"$KB_ID\"]
  }"
```

### 5. RAG 問答

```bash
curl -N -X POST "$BASE/knowledge-chat/$SESSION_ID" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"請根據知識庫回答問題\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"channel\": \"api\",
    \"disable_title\": true
  }"
```

## 限定到指定文件

業務系統裡的 `document_id` 對應 HybRAG API 中的 `knowledge_id`。

可以透過以下方式取得 `knowledge_id`：

- 上傳檔案、匯入 URL 或建立手動知識後，回應裡的 `data.id`。
- 知識庫文件列表 API 返回的每筆記錄 `id`。

在請求中使用 `knowledge_ids` 限定到一份或幾份文件：

```bash
KNOWLEDGE_ID="<knowledge_id>"

curl -sS -X POST "$BASE/knowledge-search" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"query\": \"只檢索這份文件裡的內容\",
    \"knowledge_base_ids\": [\"$KB_ID\"],
    \"knowledge_ids\": [\"$KNOWLEDGE_ID\"]
  }"
```

對於綁定知識庫範圍的個人 API Key，帶 `knowledge_ids` 時建議同時傳入對應的 `knowledge_base_ids`，用於明確驗證該文件限定發生在授權知識庫範圍內。

## 能力邊界

| 能力 | HybRAG API 支援情況 | 說明 |
| --- | --- | --- |
| 一般使用者存取自己的 RAG | 支援 | 透過個人 API Key 或 VOS/OAuth token |
| 指定 Knowledge Base | 支援 | 使用 `knowledge_base_ids` |
| 指定單個或多個文件 | 支援 | 使用 `knowledge_ids` |
| 會話復用 | 支援 | `POST /sessions` 返回 `session_id` |
| RAG Chat | 支援 | `POST /knowledge-chat/:session_id`，SSE 輸出 |
| Retrieval | 支援 | `POST /knowledge-search` |
| Citation / Sources | 支援 | 檢索結果與問答事件包含來源資訊，具體內容取決於是否命中文件 |
| API Key 建立、列表、更新、刪除 | 支援 | 一般使用者建立個人 Key，管理員可建立工作區級 Key |
| API Key 過期時間 | 支援 | 建立或更新時可設定 `expires_at_unix` |
| API Key 輪換 | 透過重建實現 | 建立新 Key、切換呼叫方、刪除舊 Key |
| NAS ACL、使用者群組、全域稽核 | 平台能力 | HybRAG 繼承 VOS 身分，但不替代 VOS/NAS 平台 API |

## 整合建議

- 服務端長期整合優先使用個人 API Key，便於綁定能力與知識庫範圍。
- 需要嚴格繼承目前 VOS 使用者權限時，使用 VOS/OAuth token。
- 終端設備不應保存 admin 憑證、平台級 API Key 或使用者密碼。
- NAS 檔案權限與 RAG 知識庫權限應分開設計：檔案可讀不代表自動進入知識庫，發布到 RAG 前應有明確流程。
