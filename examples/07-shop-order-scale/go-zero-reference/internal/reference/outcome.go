// 本文件定义落盘结束后才交给调用方的回执结果。
package reference

type outcome struct {
	order *Order
	err   error
}
