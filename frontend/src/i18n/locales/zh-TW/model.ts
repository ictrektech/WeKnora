export default {
  ollamaSettings: {
    title: 'Ollama 設定',
    description: '管理本地 Ollama 服務，查看和下載模型',
    toasts: {
      connected: '連接成功',
      connectFailed: '連接失敗，請檢查 Ollama 是否運行',
      listFailed: '獲取模型清單失敗',
      downloadFailed: '下載失敗，請稍後重試',
      downloadStarted: '已開始下載模型 {name}',
      downloadCompleted: '模型 {name} 下載完成',
      progressFailed: '查詢下載進度失敗'
    },
    installed: {
      title: '已下載的模型',
      desc: '已安裝在 Ollama 中的模型清單',
      empty: '暫無已下載的模型'
    },
    download: {
      title: '下載新模型',
      descPrefix: '輸入模型名稱下載，',
      browse: '瀏覽 Ollama 模型庫',
      placeholder: '如：qwen2.5:0.5b',
      download: '下載',
      downloading: '正在下載: {name}'
    },
    address: {
      label: '服務地址',
      desc: '本地 Ollama 服務的 API 地址，由系統自動檢測。如需修改，請在 .env 設定文件中設定',
      placeholder: 'http://localhost:11434',
      failed: '連接失敗，請檢查 Ollama 是否運行或服務地址是否正確'
    },
    status: {
      label: 'Ollama 服務狀態',
      desc: '自動檢測本地 Ollama 服務是否可用。如果服務未運行或地址設定錯誤，將顯示"不可用"狀態',
      testing: '檢測中',
      available: '可用',
      unavailable: '不可用',
      untested: '未檢測',
      retest: '重新檢測'
    }
  },
  modelCatalog: {
    title: '模型目錄',
    description: '模型目錄決定新增模型時可選的模型清單，以及脈絡視窗、是否支援思考等預設參數。在這裡修改後，所有空間立即生效。',
    howItWorks: '設定如何生效',
    layers: {
      builtin: '內建目錄：隨版本發佈的預設模型清單。',
      deployment: '部署文件：伺服器上的 models.json，可覆蓋內建目錄。',
      console: '管理員修改：在本頁所做的修改，優先於以上兩者。',
      explicit: '各空間在模型設定中填寫的參數始終優先；修改目錄不會改寫已保存的模型。'
    },
    add: '新增模型',
    more: '更多',
    jsonEditor: 'JSON 編輯',
    history: '版本歷史',
    import: '匯入 JSON',
    export: '匯出修改',
    search: '搜尋模型 ID 或名稱',
    allProviders: '全部廠商',
    allTypes: '全部類型',
    onlyModified: '只看已修改',
    summary: '{count} 個模型 · 版本 {version}',
    columns: {
      model: '模型',
      provider: '廠商',
      type: '類型',
      tokens: '上下文 / 輸出',
      capabilities: '能力',
      source: '來源'
    },
    capability: {
      reasoning: '思考',
      image: '圖像',
      audio: '音頻',
      video: '影片'
    },
    source: {
      builtin: '內建',
      deployment: '部署文件',
      console: '管理員修改'
    },
    rule: '匹配規則',
    ruleTip: '按名稱模式匹配一組模型，只提供預設參數，不會出現在候選清單中。',
    hidden: '已隱藏',
    edit: '編輯',
    view: '查看',
    empty: '沒有匹配的模型',
    editDescription: '{provider} · {type}',
    ruleNotice: '這是一條匹配規則，為名稱符合該模式的模型提供預設參數。如需修改，請使用「更多 → JSON 編輯」。',
    fieldsSection: '預設參數',
    nameDesc: '在模型選擇清單中顯示的名稱。',
    dimension: '向量維度',
    dimensionDesc: '新增該向量模型時預填的維度，需與模型實際輸出一致。',
    inputDesc: '模型可接收的輸入類型；勾選圖像後可作為視覺模型使用。',
    thinkingLevels: '思考等級',
    thinkingLevelsDesc: '對話中可選的思考強度。未勾選「關閉」表示該模型始終思考。',
    levelsNeedReasoning: '開啟「支援思考」後可設定。',
    levelsUnsupported: '該廠商介面不支援分級思考。',
    textOnly: '僅文字',
    noLevels: '無',
    sourceLink: '廠商文件',
    customHint: '這是管理員在本頁新增的模型，參數均來自這裡的設定。',
    fieldsHint: '各空間在模型設定中填寫的參數仍然優先。輸入框留空即恢復預設值。',
    context: '上下文窗口',
    contextDesc: '模型一次可處理的最大 token 數。',
    output: '最大輸出',
    outputDesc: '單次回復最多生成的 token 數。',
    reasoning: '支援思考',
    reasoningDesc: '模型能否輸出思考過程。',
    hide: '在候選清單中隱藏',
    hideDesc: '隱藏後新增模型時不再出現，已設定的模型不受影響。',
    inputModes: '輸入模態',
    inherited: '預設：{value}',
    notSet: '未設定',
    optional: '可選',
    yes: '是',
    no: '否',
    modified: '已修改',
    layersSection: '各層取值',
    layerField: '參數',
    layerBuiltin: '內建目錄',
    layerDeployment: '部署文件',
    layerEffective: '目前生效',
    save: '保存並生效',
    restore: '恢復預設',
    restoreConfirm: '移除對該模型的全部管理員修改，並立即生效？',
    restored: '已恢復，其他實例約 5 秒內同步',
    remove: '刪除模型',
    removeConfirm: '從目錄中刪除這個由管理員新增的模型，並立即生效？',
    removed: '已刪除，其他實例約 5 秒內同步',
    published: '已生效，其他實例約 5 秒內同步',
    conflict: '目錄已被其他管理員更新，已重新整理到最新版本，請重新操作。',
    loadFailed: '模型目錄載入失敗',
    deploymentError: '部署文件 models.json 載入失敗，目前只使用內建目錄：{error}',
    syncError: '最新版本同步失敗，頁面顯示的可能不是目前生效的設定：{error}',
    addDescription: '為廠商補充目錄中沒有的模型，新增後可在模型設定中直接選擇。',
    modelId: '模型 ID',
    modelIdPlaceholder: '與廠商 API 中的模型名一致，如 gpt-5-mini',
    displayName: '顯示名稱',
    required: '請選擇廠商並填寫模型 ID',
    exists: '該模型已在目錄中，請直接編輯',
    jsonDescription: '用 models.json 格式直接編輯全部管理員修改，適合批量調整或遷移。',
    jsonHint: '只寫需要修改的欄位，刪除欄位即復原預設。憑據、請求頭和環境變數引用請在模型設定或部署檔案中維護；圖示僅支援行內 SVG；最大 1 MiB。',
    jsonClear: '清空全部修改',
    jsonCheck: '檢查變更',
    jsonPublish: '發佈 {count} 項變更',
    jsonUnchanged: '與目前生效的設定一致，無需發佈。',
    changesTitle: '將要生效的變更',
    change: {
      added: '新增',
      removed: '移除',
      updated: '修改',
      provider: '廠商'
    },
    publishHint: '發佈後本實例立即生效，其他實例約 5 秒內同步。',
    invalid: '請輸入包含 providers 對象的 models.json。',
    tooLarge: '文件不能超過 1 MiB。',
    historyDescription: '保留最近 20 個版本，恢復後立即生效。',
    historyVersion: '版本 {version}',
    historyCurrent: '目前',
    historyRestore: '恢復',
    historyRestoreConfirm: '恢復到版本 {version} 的設定，並立即對所有空間生效？',
    historyEmpty: '暫無歷史版本',
    historyModels: '修改了 {count} 個模型',
    historyNoOverrides: '無管理員修改'
  },
  ollama: {
    unknown: '未知',
    today: '今天',
    yesterday: '昨天',
    daysAgo: '{days} 天前'
  },
  vectorStoreBadge: {
    systemDefault: '系統預設',
    sharedFromOrg: '來自其他組織的共享',
    unknownStore: '未知儲存',
    unavailable: '不可用'
  },
  modelSettings: {
    title: '模型設定',
    description: '管理不同類型的 AI 模型，支援 Ollama 本地模型和遠程 API',
    copySuffix: ' 副本',
    builtinTag: '內建',
    confirmDelete: '確定刪除模型「{name}」嗎？',
    usage: {
      title: '模型無法刪除',
      description: '模型「{name}」仍被以下設定引用。請先打開對應設定並更換模型，再重新刪除。',
      knowledgeBases: '知識庫（{count}）',
      agents: '智能體（{count}）',
      longTermMemory: '長期記憶',
      openConfiguration: '打開設定',
      truncated: '僅顯示前 {shown} 個，共 {total} 個',
      bindings: {
        embedding_model: 'Embedding 模型',
        summary_model: '摘要模型',
        question_model: '問題生成模型',
        image_processing_model: '圖片處理模型',
        vlm_model: '視覺理解模型',
        asr_model: '語音識別模型',
        wiki_synthesis_model: 'Wiki 綜合模型',
        auto_tag_model: '自動標籤模型',
        chat_model: '對話模型',
        rerank_model: '重排序模型',
        query_understand_model: '問題理解模型',
        follow_up_model: '追問模型',
        extract_model: '記憶提取模型',
        unknown: '其他模型設定'
      }
    },
    debug: {
      reasoningEffort: '思考強度',
      reasoningEffortDesc: '按模型目錄上報的等級發送 reasoning_effort',
      title: '模型測試',
      description: '使用已保存的模型設定發送真實請求；編輯中的修改需保存後才會生效',
      groupModel: '選擇模型',
      groupInput: '測試輸入',
      groupResult: '運行結果',
      modelType: '模型類型',
      model: '模型',
      modelPlaceholder: '請選擇要測試的模型',
      noModelsForType: '目前類型暫無已新增模型',
      query: '輸入內容',
      queryPlaceholder: '輸入要發送給模型的內容',
      embeddingInput: '待向量化文字',
      embeddingPlaceholder: '輸入要生成 Embedding 的文字',
      vlmPrompt: '圖片提示詞',
      vlmPromptPlaceholder: '例如：請詳細描述這張圖片',
      documents: '候選文件',
      documentsPlaceholder: '每行輸入一個候選文件',
      documentsHint: '每個非空行會作為一個獨立文件發送給 ReRank 模型',
      imageFile: '圖片文件',
      similarityMode: '相似度',
      similarityQuery: '查詢文字',
      similarityQueryPlaceholder: '輸入一條查詢，例如：草地上奔跑的狗',
      similarityCandidates: '候選文字',
      similarityCandidatesPlaceholder: '每行輸入一個候選文字',
      similarityHint: '查詢按檢索查詢方式向量化，候選按文件方式向量化，與線上檢索一致；結果按餘弦相似度由高到低排列',
      similarityImage: '候選圖片（可選）',
      similarityImageCandidate: '圖片',
      audioFile: '音頻文件',
      chooseFile: '選擇檔案',
      parameters: '請求參數',
      systemPrompt: 'System Prompt',
      systemPromptPlaceholder: '可選，輸入系統提示詞',
      run: '運行測試',
      copyResult: '複製結果',
      history: '歷史記錄',
      runLabel: '第 {n} 次運行',
      success: '調用成功',
      failed: '調用失敗',
      rawResponse: '響應內容',
      requestPreview: '請求預覽',
      requestFailed: '模型測試請求失敗',
      metrics: {
        api: '協議',
        thinkingFormat: '思考格式',
        requestedReasoningEffort: '請求思考強度',
        dimension: '向量維度',
        resultCount: '結果數量',
        answerChars: '回答字元數',
        reasoningChars: '推理字元數',
        reasoningReturned: '返回推理內容',
        textChars: '轉寫字元數',
        segmentCount: '分段數量'
      },
      thinking: '思考模式',
      thinkingDesc: '僅對支援思考模式的模型生效',
      thinkOn: '思考開啟',
      thinkOff: '思考關閉'
    },
    builtinModels: {
      title: '內建模型',
      description: '內建模型對所有空間可見，敏感資訊會被隱藏，且不可編輯或刪除。',
      descriptionAdmin: '內建模型對所有空間可見。系統管理員可編輯設定和憑據；刪除仍由部署設定管理。',
      viewGuide: '查看內建模型管理指南'
    },
    toasts: {
      nameRequired: '模型名稱不能為空',
      nameTooLong: '模型名稱不能超過100個字元',
      displayNameTooLong: '顯示名稱不能超過100個字元',
      baseUrlRequired: 'Remote API 類型必須填寫 Base URL',
      baseUrlInvalid: 'Base URL 格式不正確，請輸入有效的 URL',
      dimensionInvalid: 'Embedding 模型必須填寫有效的向量維度（128-4096）',
      updated: '模型已更新',
      added: '模型已新增',
      saveFailed: '保存模型失敗',
      deleted: '模型已刪除',
      deleteFailed: '刪除模型失敗',
      builtinCannotEdit: '內建模型不能編輯',
      linkedModelMissing: '找不到助手連結的模型，它可能已被刪除',
      builtinCannotDelete: '內建模型不能刪除',
      builtinCannotCopy: '內建模型不能複製',
      copied: '模型已複製',
      copyFailed: '複製模型失敗'
    },
    asr: {
      title: 'ASR 語音模型',
      desc: '設定用於語音識別和音頻轉錄的語音轉文字模型（如 OpenAI Whisper）',
      empty: '暫無 ASR 語音模型'
    },
    vllm: {
      title: 'VLLM 視覺模型',
      desc: '設定用於視覺理解和多模態的視覺語言模型',
      empty: '暫無 VLLM 視覺模型'
    },
    rerank: {
      title: 'ReRank 模型',
      desc: '設定用於結果重排序的模型',
      empty: '暫無 ReRank 模型'
    },
    embedding: {
      title: 'Embedding 模型',
      desc: '設定用於文字向量化的嵌入模型',
      empty: '暫無 Embedding 模型'
    },
    chat: {
      title: '對話模型',
      desc: '設定用於對話的大語言模型',
      empty: '暫無對話模型'
    },
    source: {
      remote: 'Remote',
      openaiCompatible: 'OpenAI兼容',
      custom: '自定義'
    },
    actions: {
      addModel: '新增模型',
      debugModel: '模型測試'
    },
    typeShort: {
      chat: '對話',
      embedding: 'Embedding',
      rerank: 'ReRank',
      vllm: '視覺',
      asr: '語音'
    },
    personalDesensitization: '個人脫敏設定',
    personalDesensitizationHint: '此設定僅對目前帳號和目前模型設定生效。名稱、地址相同的其他模型，以及其他使用者，均不會跟隨變化。NER 會在規則脫敏後執行。'
  },
  model: {
    reasoning: {
      levels: {
        off: '關閉',
        auto: '自動',
        minimal: '極低',
        low: '低',
        medium: '中',
        high: '高',
        xhigh: '極高',
        max: '最大'
      },
      levelDescriptions: {
        off: '關閉思考，不寫入任何思考參數',
        auto: '廠商預設強度，由模型自行決定思考深度',
        minimal: '最少思考，響應最快',
        low: '輕度思考',
        medium: '中等思考強度',
        high: '深度思考，響應更慢',
        xhigh: '超高強度思考（僅部分模型支援）',
        max: '最大思考預算（僅部分模型支援）'
      }
    },
    modelName: '模型名稱',
    defaultTag: '預設',
    addModelInSettings: '前往全局設定新增模型',
    loadFailed: '載入模型清單失敗',
    selectModelPlaceholder: '請選擇模型',
    searchPlaceholder: '搜尋模型...',
    builtinTag: '內建',
    editor: {
      maxOutputTokensLabel: '最大輸出 tokens',
      maxOutputTokensPlaceholder: '留空使用目錄預設',
      maxOutputTokensDesc: '單次回復的輸出上限。留空則沿用目錄中該模型的預設值。',
      catalog: {
        reasoning: '推理',
        vision: '視覺',
        hint: '可從廠商內建目錄選擇，也可直接輸入自定義模型名。'
      },
      resolved: {
        title: '實際調用方式',
        empty: '填寫廠商與模型名後，這裡會顯示這個模型將被怎樣調用',
        failed: '解析失敗',
        protocol: '請求協議',
        catalog: '能力來源',
        catalogedYes: '內建模型檔案',
        catalogedNo: '廠商通用預設（未收錄此模型）',
        endpoint: '請求地址',
        thinkingFormat: '思考開關傳參',
        thinkingLevels: '可選思考強度',
        noThinking: '該模型不支援思考'
      },
      advanced: {
        toggle: '高級',
        api: {
          label: '協議覆蓋',
          auto: '自動（按廠商 / URL 推斷）',
          desc: '強制使用指定的請求協議；一般無需修改。'
        },
        remoteModelName: {
          label: '遠端模型名',
          placeholder: '留空與模型名相同',
          desc: '實際發送給廠商的模型 ID，與上面的模型名不同時填寫。'
        },
        legacyThinking: {
          label: '思考參數格式（舊設定）',
          catalog: '遵循目錄預設（推薦）',
          none: '不寫入思考參數',
          desc: '此模型仍帶有舊版 thinking_control 設定。選擇「遵循目錄預設」後由目錄決定思考參數的寫法。'
        },
        compat: {
          label: '協議兼容覆蓋（JSON）',
          placeholder: '{\'{\'} "max_tokens_field": "max_tokens" {\'}\'}',
          desc: '只在介面與目錄預設值不一致時填寫，只寫需要改的欄位。留空表示不覆蓋。',
          docLink: '查看欄位說明',
          invalid: 'JSON 格式不正確',
          mustBeObject: '必須是 JSON 對象'
        }
      },
      addTitle: '新增模型',
      editTitle: '編輯模型',
      sectionType: '模型類型',
      typeLabel: '模型類型',
      sectionSource: '模型來源',
      sectionProvider: '接入設定',
      sectionAdvanced: '高級選項',
      sourceLabel: '模型來源',
      sourceLocal: 'Ollama',
      sourceRemote: 'API',
      baseUrlLabel: 'Base URL',
      displayNameLabel: '顯示名稱（可選）',
      displayNamePlaceholder: '例如：客服問答模型',
      displayNameDesc: '僅用於介面展示，實際調用仍使用上面的模型名稱。',
      baseUrlPlaceholder: '例如：https://api.openai.com/v1',
      baseUrlPlaceholderVllm: '例如：http://localhost:11434/v1',
      baseUrlPlaceholderAsr: '例如：https://api.openai.com/v1',
      apiKeyOptional: 'API Key（可選）',
      apiKeyPlaceholder: '輸入 API Key',
      customHeadersLabel: '自定義請求頭（可選）',
      customHeadersDesc: '調用遠程模型 API 時附加的 HTTP 請求頭，常用於企業網關鑒權、鏈路追蹤等場景；Authorization、Content-Type 等保留頭會被自動忽略。',
      customHeadersAdd: '新增請求頭',
      customHeadersKeyPlaceholder: 'Header 名稱',
      customHeadersValuePlaceholder: 'Header 值',
      testing: '測試中...',
      testConnection: '測試連接',
      downloadLabel: '下載: {keyword}',
      refreshList: '重新整理清單',
      dimensionLabel: '向量維度',
      dimensionPlaceholder: '例如：1536',
      checkDimension: '檢測維度',
      dimensionDetected: '檢測成功，向量維度：{value}',
      dimensionFailed: '檢測失敗，請手動輸入維度',
      remoteDimensionDetected: '檢測到向量維度：{value}',
      dimensionOverrideLabel: '自定義輸出維度',
      dimensionOverrideDesc: '僅在確認該模型支援 dimensions 參數時開啟；預設只使用檢測到的實際維度。',
      supportsVisionLabel: '支援視覺/多模態',
      supportsVisionDesc: '模型是否支援圖片等多模態輸入',
      embeddingImageInputLabel: '圖片輸入',
      embeddingImageInputDesc: '模型能把圖片編碼到與文字相同的向量空間。目錄內的模型會自動識別，自訂模型需手動宣告',
      contextWindowLabel: '上下文窗口',
      contextWindowPlaceholder: '預設 {value}',
      contextWindowDesc: '該模型一次請求能容納的 token 數。智能體壓縮對話歷史會按此上限工作。留空則使用預設 200000（200K）。請按廠商文件填寫真實值，填大會導致壓縮不觸發、上游直接拒絕請求。',
      contextWindowDefaultHint: '未設定，使用預設 {value}',
      contextWindowTokens: '{count} tokens',
      maxConcurrencyLabel: '後台併發上限',
      maxConcurrencyPlaceholder: '0 表示使用全局預設',
      maxConcurrencyDesc: '限制文件入庫/富化等後台任務對該模型的併發調用數（按模型全副本共享）。0 或留空表示沿用全局預設；不影響交互式對話。',
      dimensionHint: '模型已選擇，點擊"檢測維度"按鈕自動獲取向量維度',
      loadModelListFailed: '載入模型清單失敗',
      listRefreshed: '清單已重新整理',
      fillModelAndUrl: '請先填寫模型標識和 Base URL',
      remoteBaseUrlRequired: 'Remote API 類型必須填寫 Base URL',
      unsupportedModelType: '不支援的模型類型',
      saveAndClose: '保存並關閉',
      testDraftEditHint: '使用目前填寫的連接設定；API Key 使用已單獨保存的值。',
      testStale: '設定已修改，請重新測試。',
      connectionSuccess: '連接成功',
      connectionFailed: '連接失敗',
      connectionConfigError: '連接失敗，請檢查設定',
      downloadStarted: '開始下載 {name}',
      downloadCompleted: '{name} 下載完成',
      downloadFailed: '{name} 下載失敗',
      downloadStartFailed: '啟動下載失敗',
      ollamaUnavailable: 'Ollama服務不可用，無法選擇本地模型',
      ollamaNotSupportRerank: 'Ollama 不支援 ReRank 模型，請使用遠程介面設定',
      goToOllamaSettings: '查看設定',
      providerLabel: '服務商',
      providerPlaceholder: '選擇模型服務商',
      providerDocs: '查看 {provider} 的模型文件',
      validation: {
        extraFieldRequired: '請填寫 {name}',
        modelNameRequired: '請輸入模型名稱',
        modelNameEmpty: '模型名稱不能為空',
        modelNameMax: '模型名稱不能超過100個字元',
        baseUrlRequired: '請輸入 Base URL',
        baseUrlEmpty: 'Base URL 不能為空',
        baseUrlInvalid: 'Base URL 格式不正確，請輸入有效的 URL'
      },
      modelNamePlaceholder: {
        local: '例如：llama2:latest',
        remote: '例如：gpt-4, claude-3-opus',
        localVllm: '例如：llava:latest',
        remoteVllm: '例如：gpt-4-vision-preview',
        remoteAsr: '例如：whisper-1'
      },
      description: {
        chat: '設定用於對話的大語言模型',
        embedding: '設定用於文字向量化的嵌入模型',
        rerank: '設定用於結果重排序的模型',
        vllm: '設定用於視覺理解和多模態的視覺語言模型',
        asr: '設定用於語音識別和音頻轉錄的語音轉文字模型',
        default: '設定模型資訊'
      },
      testColdStartHint: '若透過本機模型服務接入，首次測試可能需要載入模型；連線測試最長等待 90 秒。',
      desensitizeRulesLabel: '開啟規則脫敏',
      desensitizeRulesDesc: '發送給該模型前，先替換手機號、電子郵件、身份證、密鑰等結構化敏感資訊。',
      desensitizeNerLabel: '開啟 NER 脫敏',
      desensitizeNerDesc: '在規則脫敏後進一步識別人名和地點；需要脫敏服務已準備 NER 模型。',
      desensitizeImageLabel: '開啟圖片脫敏',
      desensitizeImageDesc: '聊天上傳的圖片和送入該模型解析的圖片會先通過脫敏服務遮擋敏感區域；需要脫敏服務支援圖片 OCR。',
      desensitizeServiceUrlLabel: '脫敏服務地址',
      desensitizeVosHint: '同一 vos_default 網路可直接使用：',
      desensitizeFailureDesc: '啟用後若脫敏服務不可用，本次模型請求會被阻止，原文不會直接發送。',
      desensitizeServiceUrlRequired: '啟用脫敏後必須填寫脫敏服務地址',
      desensitizeServiceUrlInvalid: '脫敏服務地址格式無效',
      thinkingControlLabel: 'Thinking mode request format',
      thinkingControlDesc: 'Controls how the agent thinking-mode switch is written to the API.',
      thinkingControl: {
        none: {
          label: 'Do not send thinking fields',
          hint: 'Thinking parameters are not sent in requests.'
        },
        chatTemplateKwargs: {
          label: 'chat_template_kwargs',
          hint: 'Custom OpenAI-compatible gateways, NVIDIA NIM, vLLM / local Qwen'
        },
        enableThinking: {
          label: 'enable_thinking',
          hint: 'Alibaba DashScope thinking parameter'
        },
        thinkingType: {
          label: 'thinking.type',
          hint: 'Volcengine Ark and Tencent LKEAP thinking parameter'
        }
      },
      providers: {
        openai: {
          label: 'OpenAI',
          description: 'gpt-5.2, gpt-5-mini, etc.'
        },
        anthropic: {
          label: 'Anthropic',
          description: 'Claude models via native Anthropic Messages API'
        },
        azure_openai: {
          label: 'Azure OpenAI',
          description: 'OpenAI service hosted on Microsoft Azure'
        },
        aliyun: {
          label: 'Aliyun DashScope',
          description: 'qwen-plus, tongyi-embedding-vision-plus, qwen3-rerank, etc.'
        },
        zhipu: {
          label: 'Zhipu BigModel',
          description: 'glm-4.7, embedding-3, rerank, etc.'
        },
        openrouter: {
          label: 'OpenRouter',
          description: 'openai/gpt-5.2-chat, google/gemini-3-flash-preview, etc.'
        },
        litellm: {
          label: 'LiteLLM',
          description: 'Self-hosted proxy to 100+ providers.'
        },
        requesty: {
          label: 'Requesty',
          description: 'openai/gpt-4o-mini, anthropic/claude-sonnet-4-5, etc.'
        },
        generic: {
          label: 'Custom (OpenAI-compatible)',
          description: 'Generic API endpoint'
        },
        siliconflow: {
          label: 'SiliconFlow',
          description: 'deepseek-ai/DeepSeek-V3.1, etc.'
        },
        jina: {
          label: 'Jina',
          description: 'jina-clip-v1, jina-embeddings-v2-base-zh, etc.'
        },
        volcengine: {
          label: 'Volcengine',
          description: 'doubao-1-5-pro-32k-250115, doubao-embedding-vision-250615, etc.'
        },
        deepseek: {
          label: 'DeepSeek',
          description: 'deepseek-chat, deepseek-reasoner, etc.'
        },
        hunyuan: {
          label: 'Hunyuan',
          description: 'hunyuan-pro, hunyuan-standard, hunyuan-embedding, etc.'
        },
        minimax: {
          label: 'MiniMax',
          description: 'MiniMax-M3, MiniMax-M2.7, MiniMax-M2.7-highspeed, etc.'
        },
        mimo: {
          label: 'MiMo',
          description: 'mimo-v2-flash'
        },
        gemini: {
          label: 'Google Gemini',
          description: 'gemini-3-flash-preview, gemini-2.5-pro, etc.'
        },
        gpustack: {
          label: 'GPUStack',
          description: 'Choose your deployed model on GPUStack'
        },
        modelscope: {
          label: 'ModelScope',
          description: 'Qwen/Qwen3-8B, Qwen/Qwen3-Embedding-8B, etc.'
        },
        qiniu: {
          label: 'Qiniu Cloud',
          description: 'deepseek/deepseek-v3.2-251201, z-ai/glm-4.7, etc.'
        },
        moonshot: {
          label: 'Moonshot',
          description: 'kimi-k2-turbo-preview, moonshot-v1-8k-vision-preview, etc.'
        },
        qianfan: {
          label: 'Baidu Qianfan',
          description: 'ernie-5.0-thinking-preview, embedding-v1, bce-reranker-base, etc.'
        },
        longcat: {
          label: 'LongCat AI',
          description: 'LongCat-Flash-Chat, LongCat-Flash-Thinking, etc.'
        },
        lkeap: {
          label: 'Tencent Cloud LKEAP',
          description: 'DeepSeek-R1, DeepSeek-V3, lke-reranker-base, etc.'
        },
        nvidia: {
          label: 'NVIDIA',
          description: 'deepseek-ai-deepseek-v3_1, nv-embed-v1, rerank-qa-mistral-4b, etc.'
        },
        novita: {
          label: 'Novita AI',
          description: 'moonshotai/kimi-k2.5, zai-org/glm-5, minimax/minimax-m2.7, qwen/qwen3-embedding-0.6b, etc.'
        }
      },
      volcengine: {
        accessKeyLabel: 'Access Key ID',
        accessKeyPlaceholder: 'Volcengine Access Key ID',
        secretKeyLabel: 'Secret Access Key',
        secretKeyPlaceholder: 'Volcengine Secret Access Key',
        rerankCredentialHint: 'Rerank uses VikingDB AK/SK signing, not an Ark API key.'
      },
      lkeap: {
        secretIdLabel: 'SecretId',
        secretIdPlaceholder: 'Tencent Cloud API SecretId',
        secretKeyLabel: 'SecretKey',
        secretKeyPlaceholder: '騰訊雲 API 密鑰 SecretKey',
        regionLabel: 'Region',
        regionPlaceholder: 'ap-guangzhou',
        regionDesc: 'RunRerank supports ap-beijing, ap-guangzhou, etc. Default: ap-guangzhou',
        rerankCredentialHint: 'Rerank uses Tencent Cloud API signature. Create SecretId/SecretKey in the CAM console.'
      }
    }
  },
  modelHubDownload: {
    title: 'Model Hub 正在準備模型',
    starting: '正在觸發下載…',
    failed: '下載失敗',
    retry: '重試',
    unavailable: '正在等待 Model Hub 連接…'
  }
}
