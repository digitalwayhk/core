package adapter

import (
	"testing"
)

func TestSplitRange(t *testing.T) {
	rangeList := getPageList(10000, 200)
	println(rangeList)
}
