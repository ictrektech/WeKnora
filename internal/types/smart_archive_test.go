package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveImportItemDefaultsAndTaskPayload(t *testing.T) {
	item := &ArchiveImportItem{TenantID: 7, BatchID: "batch-1", FileHash: "hash"}
	require.NoError(t, item.BeforeCreate(nil))
	require.NotEmpty(t, item.ID)
	require.Equal(t, ArchiveImportItemQueued, item.Status)
	require.Equal(t, ArchiveDefaultExtractionVersion, item.ExtractionVersion)

	payload := ArchiveImportTaskPayload{
		TenantID:          item.TenantID,
		BatchID:           item.BatchID,
		ItemID:            item.ID,
		RunID:             "run-1",
		FileHash:          item.FileHash,
		ExtractionVersion: item.ExtractionVersion,
	}
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	var decoded ArchiveImportTaskPayload
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, payload, decoded)
}

func TestSmartArchiveCreateHooksInitializeDurableDefaults(t *testing.T) {
	settings := &ArchiveSettings{}
	require.NoError(t, settings.BeforeCreate(nil))
	require.NotEmpty(t, settings.ID)
	require.Equal(t, "Asia/Shanghai", settings.Timezone)
	require.Equal(t, ArchiveDefaultExtractionVersion, settings.ExtractionVersion)
	require.Equal(t, 30, settings.TrashRetentionDays)

	document := &ArchiveDocument{}
	require.NoError(t, document.BeforeCreate(nil))
	require.NotEmpty(t, document.ID)
	require.Equal(t, ArchiveDocumentOther, document.DocumentType)
	require.Equal(t, ArchiveBusinessOther, document.BusinessType)
	require.Equal(t, ArchiveExtractionUploading, document.ExtractionStatus)
	require.Equal(t, ArchiveDefaultExtractionVersion, document.ExtractionVersion)
	require.JSONEq(t, `{}`, string(document.ExtractedFields))
	require.JSONEq(t, `{}`, string(document.Metadata))

	evidence := &ArchiveFieldEvidence{}
	require.NoError(t, evidence.BeforeCreate(nil))
	require.NotEmpty(t, evidence.ID)
	require.Equal(t, ArchiveLocatorText, evidence.LocatorKind)
	require.JSONEq(t, `{}`, string(evidence.Locator))
	require.Equal(t, "archive_field_evidence", evidence.TableName())
}
