package util

import "runtime"

const (
	gBufSize = 4 * 1024 * 1024
)

var RuntimeUtil = runtimeUtil{}

type runtimeUtil struct {
}

func (runtimeUtil) GetStack() string {
	buf := make([]byte, gBufSize)
	buf = buf[:runtime.Stack(buf, false)]
	return string(buf)
}
