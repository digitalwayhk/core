package mq

import (
	"strings"
	"testing"
)

func TestNATSResourceNameIsValidDeterministicAndCollisionResistant(t *testing.T) {
	first := natsResourceName("core.integration", "orders.created/v1")
	second := natsResourceName("core.integration", "orders.created/v1")
	if first != second {
		t.Fatalf("资源名必须确定: %q != %q", first, second)
	}
	for _, forbidden := range []string{".", "/", "*", ">", " ", "\\"} {
		if contains := strings.Contains(first, forbidden); contains {
			t.Fatalf("资源名包含 NATS 禁止字符 %q: %q", forbidden, first)
		}
	}
	if first == natsResourceName("core.integration", "orders/created.v1") {
		t.Fatalf("不同 subject 不得因字符清洗发生资源名冲突: %q", first)
	}
}
