// 本文件保留 Snowflake worker 的兼容构造入口。
package utils

import (
	"strconv"

	"github.com/yitter/idgenerator-go/idgen"
)

// NewAlgorithmSnowFlake 按旧十进制拼接规则组合 machineId 和 dataCenterId。
func NewAlgorithmSnowFlake(machineId uint, dataCenterId uint) idgen.ISnowWorker {
	d := strconv.Itoa(int(dataCenterId))
	m := strconv.Itoa(int(machineId))
	dm, _ := strconv.Atoi(d + m)
	var options = idgen.NewIdGeneratorOptions(uint16(dm))
	return idgen.NewSnowWorkerM1(options)
}
