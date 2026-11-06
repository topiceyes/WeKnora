package handler

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestUpdatedIMCredentials(t *testing.T) {
	existing := types.JSON(`{"client_id":"bot-1","client_secret":"existing-secret"}`)
	for _, body := range []string{"", "{}", "{  }", "null"} {
		t.Run("preserve_"+body, func(t *testing.T) {
			var incoming types.JSON
			if body != "" {
				incoming = types.JSON(body)
			}
			got, err := updatedIMCredentials(existing, incoming)
			require.NoError(t, err)
			require.JSONEq(t, string(existing), string(got), "editing a summary must retain the bot credentials")
		})
	}

	rotated := types.JSON(`{"client_id":"bot-1","client_secret":"rotated-secret"}`)
	got, err := updatedIMCredentials(existing, rotated)
	require.NoError(t, err)
	require.JSONEq(t, string(rotated), string(got))

	for _, body := range []string{`[]`, `"secret"`, `42`, `{`} {
		_, err := updatedIMCredentials(existing, types.JSON(body))
		require.Error(t, err, "malformed credentials must not replace working credentials")
	}
}
