// 本文件只定义同一归属租约内的组占用身份；不作为业务或持久消息标识。
package ownership

type groupWorkIdentity struct{ scope, key, group string }
