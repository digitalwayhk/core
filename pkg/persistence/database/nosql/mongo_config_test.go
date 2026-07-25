package nosql

import (
	"fmt"
	"testing"
)

func TestMongoURIKeepsAdminAuthenticationCompatibility(t *testing.T) {
	got := fmt.Sprintf(mongoUri, "core_test", "secret", "127.0.0.1", 27017)
	want := "mongodb://core_test:secret@127.0.0.1:27017"
	if got != want {
		t.Fatalf("MongoDB 连接串应保持默认 admin 认证兼容: got=%q want=%q", got, want)
	}
}
