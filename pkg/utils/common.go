/*
 * @Author: vincent
 * @Date: 2021-09-11 13:03:24
 * @Description: 通用函数，处理文件，获取当前运行路径等
 */
package utils

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/mail"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func PrintObj(o interface{}) string {
	ot := reflect.TypeOf(o)
	ntype := ot.Kind()
	if ntype == reflect.Array || ntype == reflect.Slice {
		s := reflect.ValueOf(o)
		var ss string
		for i := 0; i < s.Len(); i++ {
			oo := s.Index(i).Interface()
			b, _ := json.Marshal(oo)
			ss := string(b)
			fmt.Println(ss)
		}
		return ss
	} else {
		b, _ := json.Marshal(o)
		s := string(b)
		return s
	}
}

func GetRandNum(n int) int {
	rand.Seed(time.Now().Unix())
	return rand.Intn(n)
}

// 字符时间戳转时间
func ToTime(s string) time.Time {
	t, _ := strconv.ParseInt(s, 10, 64)
	tme := time.Unix(t/1000, 0)
	return tme
}

func Md5(strs ...string) string {
	str := ""
	for _, s := range strs {
		str += s
	}
	data := []byte(str)
	has := md5.Sum(data)
	md5str := fmt.Sprintf("%x", has)
	return md5str
}

// UserIDHash - 用户ID哈希（32字符，推荐用于用户标识）
func UserIDHash(externalID string) string {
	hash := sha256.Sum256([]byte(externalID))
	return hex.EncodeToString(hash[:16]) // 32字符，2^128种可能
}

// UserIDUUID - UUID格式用户ID（36字符，标准格式）
func UserIDUUID(externalID string) string {
	namespace := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	return uuid.NewSHA1(namespace, []byte(externalID)).String()
}

// ===== 通用哈希函数 =====

// ShortHash - 短哈希（16字符）
func ShortHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:8])
}

// MediumHash - 中等哈希（32字符）
func MediumHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:16])
}

// SecureHash - 完整哈希（64字符）
func SecureHash(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}

// 保持向后兼容
func HashCodeHex(s string) string {
	return SecureHash(s)
}

// HashCode64 - 返回64位整数
func HashCode64(s string) uint64 {
	hash := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint64(hash[:8])
}

func HashCodes(strings ...string) string {
	var buf bytes.Buffer

	for _, s := range strings {
		buf.WriteString(fmt.Sprintf("%s-", s))
	}
	return MediumHash(buf.String())
}

func IsTest() bool {
	return flag.Lookup("test.v") != nil || flag.Lookup("testify.m") != nil
}

// FirstUpper 字符串首字母大写
func FirstUpper(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// FirstLower 字符串首字母小写
func FirstLower(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// func hash[A comparable](a A) uintptr {
// 	var m interface{} = make(map[A]struct{})
// 	hf := (mh)((*unsafe.Pointer)(unsafe.Pointer(&m))).hf
// 	return hf(unsafe.Pointer(&a), 0)
// }

// mh is an inlined combination of runtime._type and runtime.maptype.
// type mh struct {
// 	hf func(unsafe.Pointer, uintptr) uintptr
// }

func IsEmail(device string) bool {
	if _, err := mail.ParseAddress(device); err == nil {
		return true
	}
	return false
}

func IsMobile(device string) bool {
	if !strings.Contains(device, " ") {
		return false
	}
	phone := strings.Split(device, " ")
	areaCode, phoneNo := phone[0], phone[1]
	if strings.HasPrefix(areaCode, "+") {
		areaCode = TrimFirstRune(areaCode)
	}
	phoneNo = areaCode + phoneNo
	if _, err := strconv.Atoi(phoneNo); err == nil {
		return true
	}
	return false
}

func TrimFirstRune(s string) string {
	_, i := utf8.DecodeRuneInString(s)
	return s[i:]
}
