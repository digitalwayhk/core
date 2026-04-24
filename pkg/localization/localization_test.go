package localization

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInfo_SimpleMessage(t *testing.T) {
	info := GetInfo(nil, "hello world")
	assert.Equal(t, Info("hello world"), info)
}

func TestGetInfo_FormatArgs(t *testing.T) {
	info := GetInfo(nil, "user %s has %d items", "alice", 5)
	assert.Equal(t, Info("user alice has 5 items"), info)
}

func TestGetInfo_WithSender(t *testing.T) {
	sender := struct{ Name string }{Name: "test"}
	info := GetInfo(sender, "from %s", "sender")
	assert.Equal(t, Info("from sender"), info)
}

func TestGetInfo_EmptyFormat(t *testing.T) {
	info := GetInfo(nil, "")
	assert.Equal(t, Info(""), info)
}

func TestGetErrorInfo_BasicError(t *testing.T) {
	err := errors.New("something went wrong")
	ei := GetErrorInfo(nil, err)
	require.NotNil(t, ei)
	assert.Equal(t, "something went wrong", ei.Msg)
}

func TestGetErrorInfo_WithSender(t *testing.T) {
	sender := "some-service"
	err := errors.New("connection timeout")
	ei := GetErrorInfo(sender, err)
	require.NotNil(t, ei)
	assert.Equal(t, "connection timeout", ei.Msg)
}

func TestInfo_IsString(t *testing.T) {
	var i Info = "test info"
	assert.Equal(t, "test info", string(i))
}

func TestErrorInfo_Fields(t *testing.T) {
	ei := &ErrorInfo{Msg: "custom error message"}
	assert.Equal(t, "custom error message", ei.Msg)
}
