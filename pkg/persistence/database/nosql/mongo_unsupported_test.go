package nosql

import (
	"errors"
	"testing"
)

func TestMongoUnsupportedOperationsReturnExplicitErrors(t *testing.T) {
	mongo := &Mongo{}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{name: "transaction", run: mongo.Transaction, want: ErrMongoTransactionsUnsupported},
		{name: "commit", run: mongo.Commit, want: ErrMongoTransactionsUnsupported},
		{name: "rollback", run: mongo.Rollback, want: ErrMongoTransactionsUnsupported},
		{name: "raw", run: func() error { return mongo.Raw("query", nil) }, want: ErrMongoRawUnsupported},
		{name: "exec", run: func() error { return mongo.Exec("query", nil) }, want: ErrMongoRawUnsupported},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.want) {
				t.Fatalf("应返回 %v，实际为 %v", tt.want, err)
			}
		})
	}
}
