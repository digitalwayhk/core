package basedata

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestSupplierCodeAndNameAreUniqueWithinSupplierTable(t *testing.T) {
	utils.TESTPATH = t.TempDir()

	first := NewSupplier()
	first.SetID(7101)
	first.Code = "unique-supplier"
	first.Name = "唯一供应商"
	require.NoError(t, first.Insert())

	duplicateCode := NewSupplier()
	duplicateCode.SetID(7102)
	duplicateCode.Code = " UNIQUE-SUPPLIER "
	duplicateCode.Name = "另一个供应商"
	require.ErrorContains(t, duplicateCode.Insert(), "供应商编码或名称不能重复")

	duplicateName := NewSupplier()
	duplicateName.SetID(7103)
	duplicateName.Code = "another-supplier"
	duplicateName.Name = "唯一供应商"
	require.ErrorContains(t, duplicateName.Insert(), "供应商编码或名称不能重复")
}
