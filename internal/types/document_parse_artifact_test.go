package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentParseArtifactDefaults(t *testing.T) {
	artifact := &DocumentParseArtifact{}
	require.NoError(t, artifact.BeforeCreate(nil))
	require.NotEmpty(t, artifact.ID)
	require.Equal(t, DocumentParseArtifactVersion, artifact.ParserVersion)
}
