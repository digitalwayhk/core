package models

import (
	"os"
	"testing"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shop-user-test-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestAddressOwnershipUsesTrustedUser(t *testing.T) {
	_, err := EnsureUser("buyer-a", "用户 A")
	require.NoError(t, err)
	item := NewAddress()
	item.SetID(1001)
	item.UserID = "buyer-a"
	item.Recipient = "用户 A"
	item.Detail = "1 号"
	require.NoError(t, InsertAddress(item))

	owned, err := FindOwnedAddress("buyer-a", item.ID)
	require.NoError(t, err)
	assert.Equal(t, "1 号", owned.Detail)
	foreign, err := FindOwnedAddress("buyer-b", item.ID)
	assert.Nil(t, foreign)
	assert.NoError(t, err)
}
