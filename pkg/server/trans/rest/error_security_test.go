package rest

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteErrorResponseDoesNotDiscloseCause(t *testing.T) {
	recorder := httptest.NewRecorder()
	internal := errors.New("database password=private-database-secret")

	writeErrorResponse(recorder, StatusInternalServerError, "request failed", internal)

	require.Equal(t, StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "private-database-secret")
	var response ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	if response.Error != nil {
		require.Empty(t, response.Error.Details)
	}
}
