package types

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type authLifecycleHooks struct{}

func (*authLifecycleHooks) OnAuth(context.Context, *AuthHookArgs) error { return nil }

func (*authLifecycleHooks) OnAuthRequest(context.Context, AuthRequestArgs) error { return nil }

func (*authLifecycleHooks) OnCasdoorEvent(context.Context, CasdoorEvent) error { return nil }

func TestAuthLifecycleHookContractsCanBeImplementedTogether(t *testing.T) {
	var service interface{} = &authLifecycleHooks{}
	_, issueOK := service.(IAuthHookProvider)
	_, requestOK := service.(IAuthRequestHookProvider)
	_, eventOK := service.(ICasdoorEventHookProvider)
	require.True(t, issueOK)
	require.True(t, requestOK)
	require.True(t, eventOK)
}

func TestCloneAuthClaimsDoesNotShareNestedMutableValues(t *testing.T) {
	original := map[string]interface{}{
		"role": "buyer",
		"nested": map[string]interface{}{
			"tenant": "tenant-a",
		},
		"scopes": []interface{}{"read", "write"},
	}

	cloned := CloneAuthClaims(original)
	cloned["role"] = "admin"
	cloned["nested"].(map[string]interface{})["tenant"] = "tenant-b"
	cloned["scopes"].([]interface{})[0] = "delete"

	require.Equal(t, "buyer", original["role"])
	require.Equal(t, "tenant-a", original["nested"].(map[string]interface{})["tenant"])
	require.Equal(t, "read", original["scopes"].([]interface{})[0])
}

func TestCasdoorEventContainsOnlyNormalizedIdentityFields(t *testing.T) {
	event := CasdoorEvent{
		ID:              "evt-1",
		ServiceName:     "shop",
		AuthType:        AuthTypeUser,
		Provider:        AuthProviderCasdoor,
		ProviderSubject: "alice",
		UID:             "user-1",
		EventType:       "logout",
		EventOrder:      12,
		Generation:      7,
		OccurredAt:      time.Unix(1_900_000_000, 0).UTC(),
	}
	require.Equal(t, "alice", event.ProviderSubject)
	require.Equal(t, uint64(7), event.Generation)
}
