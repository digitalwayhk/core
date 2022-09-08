package utils

import (
	"strconv"

	"github.com/yitter/idgenerator-go/idgen"
)

//NewAlgorithmSnowFlake machineId,dataCenterId
func NewAlgorithmSnowFlake(machineId uint, dataCenterId uint) idgen.ISnowWorker {
	d := strconv.Itoa(int(dataCenterId))
	m := strconv.Itoa(int(machineId))
	dm, _ := strconv.Atoi(d + m)
	var options = idgen.NewIdGeneratorOptions(uint16(dm))
	return idgen.NewSnowWorkerM1(options)
}
