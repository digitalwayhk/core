package nosql

import (
	"os"
	"testing"
)

func requireMySQLIntegration(t *testing.T) {
	t.Helper()
	if !persistenceIntegrationBuild || os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("MySQL 集成测试需要 -tags=integration 且 CORE_TEST_MYSQL=1")
	}
}
