package identity

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityEventRecordUsesEventIDAsStableHash(t *testing.T) {
	record := NewIdentityEventRecord()
	record.EventID = "  event-001  "
	record.AuthType = "auth"
	record.UserID = "user-1"
	record.EventType = "logout"
	record.Generation = 2
	record.Blocked = true
	record.OccurredAt = time.Unix(1_700_000_000, 0).UTC()

	require.NoError(t, record.Normalize())
	require.NotNil(t, record.BusinessModel)
	require.NotNil(t, record.ShopModel)
	require.NotNil(t, record.Model)
	assert.Equal(t, "event-001", record.EventID)
	assert.NotEmpty(t, record.GetHash())

	typeOf := reflect.TypeOf(*record)
	for _, forbidden := range []string{"Token", "Secret", "Header", "Payload", "Claims"} {
		_, exists := typeOf.FieldByName(forbidden)
		assert.False(t, exists, "身份事件审计不得保存%s", forbidden)
	}
}

func TestIdentityEventRecordRejectsIncompleteEvent(t *testing.T) {
	record := NewIdentityEventRecord()
	require.ErrorContains(t, record.Normalize(), "事件 ID")
}
