//go:build windows || plan9 || js || wasip1 || aix

// 本文件保留尚未验证平台的原Badger行为，禁止套用其它平台锁或在无排他保护时修改文件。
package badgeropen

func recoverEmptyMemtables(string) (int, error) { return 0, nil }
