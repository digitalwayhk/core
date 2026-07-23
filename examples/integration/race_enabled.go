//go:build race

package integration

// IsRaceRun 判断当前测试二进制是否由 go test -race 构建。
func IsRaceRun() bool { return true }
