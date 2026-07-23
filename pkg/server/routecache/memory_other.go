//go:build !darwin && !linux

package routecache

func physicalMemoryBytes() int64 { return 0 }
