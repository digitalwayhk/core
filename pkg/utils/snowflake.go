// 本文件保留 Snowflake worker 的兼容构造入口。
package utils

import (
	"fmt"
	"strconv"

	"github.com/yitter/idgenerator-go/idgen"
)

// StableSnowflakeWorkerID 保留十进制旧编码，同时拒绝超出实际六位空间的 worker。
// 不同配置可能映射到相同 worker，调用方仍须以返回值作为唯一分配键。
func StableSnowflakeWorkerID(machineID, dataCenterID uint) (uint16, error) {
	value := strconv.FormatUint(uint64(dataCenterID), 10) + strconv.FormatUint(uint64(machineID), 10)
	worker, err := strconv.ParseUint(value, 10, 16)
	if err != nil || worker > 63 {
		return 0, fmt.Errorf("stable snowflake: datacenter=%d machine=%d exceeds 6-bit worker space", dataCenterID, machineID)
	}
	return uint16(worker), nil
}

// NewAlgorithmSnowFlake 按旧十进制拼接规则组合 machineId 和 dataCenterId。
func NewAlgorithmSnowFlake(machineId uint, dataCenterId uint) idgen.ISnowWorker {
	d := strconv.Itoa(int(dataCenterId))
	m := strconv.Itoa(int(machineId))
	dm, _ := strconv.Atoi(d + m)
	var options = idgen.NewIdGeneratorOptions(uint16(dm))
	return idgen.NewSnowWorkerM1(options)
}
