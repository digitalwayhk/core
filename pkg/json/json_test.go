package json

import (
	"reflect"
	"testing"
)

type sample struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	in := sample{Name: "alice", Age: 30}
	data, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(data) != `{"name":"alice","age":30}` {
		t.Fatalf("Marshal output = %s", data)
	}
	var out sample
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch: in=%+v out=%+v", in, out)
	}
}

func TestMarshalToString(t *testing.T) {
	s, err := MarshalToString(sample{Name: "bob", Age: 7})
	if err != nil {
		t.Fatal(err)
	}
	if s != `{"name":"bob","age":7}` {
		t.Fatalf("MarshalToString = %s", s)
	}
}

func TestUnmarshalFromString(t *testing.T) {
	var out sample
	if err := UnmarshalFromString(`{"name":"carol","age":42}`, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "carol" || out.Age != 42 {
		t.Fatalf("UnmarshalFromString got %+v", out)
	}
}

func TestUnmarshal_Error(t *testing.T) {
	var out sample
	if err := Unmarshal([]byte("not json"), &out); err == nil {
		t.Fatal("expected error unmarshalling invalid JSON")
	}
	if err := UnmarshalFromString("also not json", &out); err == nil {
		t.Fatal("expected error from UnmarshalFromString")
	}
}

func TestMarshal_Error(t *testing.T) {
	// Channels can't be marshaled; expect an error.
	if _, err := Marshal(make(chan int)); err == nil {
		t.Fatal("expected error marshalling chan")
	}
	if _, err := MarshalToString(make(chan int)); err == nil {
		t.Fatal("expected error from MarshalToString for chan")
	}
}
