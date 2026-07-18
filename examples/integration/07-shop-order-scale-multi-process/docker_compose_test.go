// Package shoporderscalemultiprocess 验证 07 Docker 水平扩容模板不会破坏本地 pending 隔离。
package shoporderscalemultiprocess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDockerScaleServiceDoesNotPublishOrderPorts 验证可扩容 order 模板不暴露宿主机业务端口。
func TestDockerScaleServiceDoesNotPublishOrderPorts(t *testing.T) {
	content := read07Compose(t)
	scaleBlock := composeServiceBlock(content, "shop-order")
	require.NotContains(t, scaleBlock, "ports:")
	require.NotContains(t, scaleBlock, "\"-p\"")
	require.NotContains(t, scaleBlock, "\"-grpc\"")
	require.Contains(t, scaleBlock, "SHOP_LOCAL_PENDING_DIR: /data/pending")
	require.NotContains(t, scaleBlock, "SHOP_ADVERTISE_ADDRESS:")
}

// TestPublicCancelPayDoNotWriteOutboxTwice 防止 Public 层绕过 business 再写第二条 Outbox。
func TestPublicCancelPayDoNotWriteOutboxTwice(t *testing.T) {
	for _, file := range []string{
		filepath.Join("..", "..", "07-shop-order-scale", "order-service", "api", "public", "cancel_order.go"),
		filepath.Join("..", "..", "07-shop-order-scale", "order-service", "api", "public", "create_payment.go"),
	} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NotContains(t, string(data), "WriteOrderChanged"+"Outbox")
	}
}

// TestDockerScaleServiceDoesNotSharePendingVolume 验证 --scale 模板不把多个副本挂到同一个 pending 卷。
func TestDockerScaleServiceDoesNotSharePendingVolume(t *testing.T) {
	content := read07Compose(t)
	scaleBlock := composeServiceBlock(content, "shop-order")
	require.NotContains(t, scaleBlock, "volumes:")
	require.NotContains(t, content, "order-scale-pending")
	require.Contains(t, content, "order-a-pending")
	require.Contains(t, content, "order-b-pending")
}

// TestDockerOrderReplicasUseInternalMySQL 验证所有 order 副本共享内部 MySQL 权威库。
func TestDockerOrderReplicasUseInternalMySQL(t *testing.T) {
	content := read07Compose(t)
	require.Contains(t, content, "mysql:")
	require.Contains(t, content, "MYSQL_DATABASE: shop_order_scale_remote")
	for _, service := range []string{"shop-order-a", "shop-order-b", "shop-order"} {
		block := composeServiceBlock(content, service)
		require.Contains(t, block, "SHOP_ORDER_REMOTE_MYSQL_HOST: mysql")
		require.Contains(t, block, "SHOP_ORDER_REMOTE_MYSQL_DATABASE: shop_order_scale_remote")
		require.NotContains(t, block, "ports:")
	}
}

// read07Compose 读取 07 Docker Compose 文件。
func read07Compose(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "07-shop-order-scale", "deploy", "docker-compose.yml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// composeServiceBlock 提取指定服务的 YAML 片段，用于轻量检查扩容约束。
func composeServiceBlock(content, service string) string {
	startMarker := "\n  " + service + ":\n"
	start := strings.Index("\n"+content, startMarker)
	if start < 0 {
		return ""
	}
	block := ("\n" + content)[start+1:]
	lines := strings.Split(block, "\n")
	result := []string{}
	for index, line := range lines {
		if index > 0 && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if index > 0 && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			break
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}
