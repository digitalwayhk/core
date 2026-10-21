// 本文件验证持续归属能力必须明确声明，不能从单主题SharedKeys推断。
package mq

import "testing"

func TestRedisDeclaresContinuousOwnership(t *testing.T) {
	capability, ok := any(&RedisStreamProvider{}).(interface{ SupportsScopedOwnership() bool })
	if !ok || !capability.SupportsScopedOwnership() {
		t.Fatal("Redis只有单主题执行租约，未交付跨主题持续归属合同")
	}
}

func TestNATSDoesNotClaimContinuousOwnership(t *testing.T) {
	capability, ok := any(&NATSJetStreamProvider{}).(interface{ SupportsScopedOwnership() bool })
	if ok && capability.SupportsScopedOwnership() {
		t.Fatal("NATS不能把普通可靠ACK声明为持续归属")
	}
}
