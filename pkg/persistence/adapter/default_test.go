package adapter

import (
	"testing"
)

func TestSplitRange(t *testing.T) {
	rangeList := splitRange(10000, 200)
	println(rangeList)
}
