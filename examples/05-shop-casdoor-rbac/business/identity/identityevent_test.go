package identity

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestIdentityEventServiceRecordsRepeatedEventOnce(t *testing.T) {
	utils.TESTPATH = t.TempDir()
	service := NewIdentityEventService()
	event := types.CasdoorEvent{
		ID: "event-idempotent-001", ServiceName: "casdoorrbacshop",
		AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor,
		ProviderSubject: "alice", UID: "user-1", EventType: "logout",
		EventOrder: 1_700_000_000, Generation: 3, Blocked: true,
		OccurredAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	require.NoError(t, service.Record(context.Background(), event))
	require.NoError(t, service.Record(context.Background(), event))

	records, err := models.NewIdentityEventRecord().QueryByEventID(event.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, event.ID, records[0].EventID)
	require.Equal(t, event.UID, records[0].UserID)
	require.Equal(t, event.Generation, records[0].Generation)
}
