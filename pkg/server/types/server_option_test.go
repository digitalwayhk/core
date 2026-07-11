package types

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestServerOptionCloneReturnsDeepCopy(t *testing.T) {
	fileSystem := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("demo")}}
	original := &ServerOption{
		IsCors:     true,
		OriginCors: []string{"https://example.com"},
		WhiteList:  []string{"127.0.0.1"},
		Demo:       &DemoOption{Pattern: "demo", File: fileSystem},
		Trans:      &TransOption{IsRest: true, RetryCount: 3},
		Quic:       &QuicOption{IsQuic: true, CertFile: "cert.pem"},
	}

	cloned := original.Clone()
	cloned.IsCors = false
	cloned.OriginCors[0] = "mutated"
	cloned.WhiteList[0] = "mutated"
	cloned.Demo.Pattern = "mutated"
	cloned.Trans.RetryCount = 99
	cloned.Quic.CertFile = "mutated.pem"

	if !original.IsCors || original.OriginCors[0] != "https://example.com" || original.WhiteList[0] != "127.0.0.1" {
		t.Fatal("Clone exposed ordinary or slice fields")
	}
	if original.Demo.Pattern != "demo" || original.Trans.RetryCount != 3 || original.Quic.CertFile != "cert.pem" {
		t.Fatal("Clone exposed pointer fields")
	}
	data, err := fs.ReadFile(cloned.Demo.File, "index.html")
	if err != nil || string(data) != "demo" {
		t.Fatalf("Clone did not preserve Demo.File reference: data=%q err=%v", data, err)
	}
}

func TestNilServerOptionClone(t *testing.T) {
	var option *ServerOption
	if option.Clone() != nil {
		t.Fatal("nil ServerOption Clone should return nil")
	}
}
