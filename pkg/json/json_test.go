package json

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
	Flag  bool   `json:"flag"`
}

func TestMarshal(t *testing.T) {
	obj := testStruct{Name: "hello", Value: 42, Flag: true}
	b, err := Marshal(obj)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"name":"hello"`)
	assert.Contains(t, string(b), `"value":42`)
	assert.Contains(t, string(b), `"flag":true`)
}

func TestMarshal_Nil(t *testing.T) {
	b, err := Marshal(nil)
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

func TestMarshal_Slice(t *testing.T) {
	items := []testStruct{
		{Name: "a", Value: 1},
		{Name: "b", Value: 2},
	}
	b, err := Marshal(items)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"name":"a"`)
	assert.Contains(t, string(b), `"name":"b"`)
}

func TestUnmarshal(t *testing.T) {
	data := []byte(`{"name":"world","value":99,"flag":false}`)
	var obj testStruct
	err := Unmarshal(data, &obj)
	require.NoError(t, err)
	assert.Equal(t, "world", obj.Name)
	assert.Equal(t, 99, obj.Value)
	assert.False(t, obj.Flag)
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	var obj testStruct
	err := Unmarshal([]byte(`not json`), &obj)
	assert.Error(t, err)
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	original := testStruct{Name: "round-trip", Value: 123, Flag: true}
	b, err := Marshal(original)
	require.NoError(t, err)

	var result testStruct
	err = Unmarshal(b, &result)
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

func TestMarshalToString(t *testing.T) {
	obj := testStruct{Name: "str", Value: 7}
	s, err := MarshalToString(obj)
	require.NoError(t, err)
	assert.Contains(t, s, `"name":"str"`)
	assert.Contains(t, s, `"value":7`)
}

func TestUnmarshalFromString(t *testing.T) {
	s := `{"name":"fromstr","value":55,"flag":true}`
	var obj testStruct
	err := UnmarshalFromString(s, &obj)
	require.NoError(t, err)
	assert.Equal(t, "fromstr", obj.Name)
	assert.Equal(t, 55, obj.Value)
	assert.True(t, obj.Flag)
}

func TestUnmarshalFromString_Invalid(t *testing.T) {
	var obj testStruct
	err := UnmarshalFromString(`{bad json}`, &obj)
	assert.Error(t, err)
}

func TestMarshalToString_UnmarshalFromString_RoundTrip(t *testing.T) {
	original := testStruct{Name: "trip", Value: 321, Flag: false}
	s, err := MarshalToString(original)
	require.NoError(t, err)

	var result testStruct
	err = UnmarshalFromString(s, &result)
	require.NoError(t, err)
	assert.Equal(t, original, result)
}
