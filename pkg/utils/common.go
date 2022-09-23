/*
 * @Author: vincent
 * @Date: 2021-09-11 13:03:24
 * @Description: 通用函数，处理文件，获取当前运行路径等
 */
package utils

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"flag"
	"fmt"
	"hash/crc32"
	"math/rand"
	"net/mail"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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

//字符时间戳转时间
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

// String hashes a string to a unique HashCode.
// https://github.com/hashicorp/terraform/blob/master/helper/hashcode/hashcode.go
// crc32 returns a uint32, but for our use we need
// and non negative integer. Here we cast to an integer
// and invert it if the result is negative.
func HashCode(s string) int {
	v := int(crc32.ChecksumIEEE([]byte(s)))
	if v >= 0 {
		return v
	}
	if -v >= 0 {
		return -v
	}
	// v == MinInt
	return 0
}

func HashCodes(strings ...string) string {
	var buf bytes.Buffer

	for _, s := range strings {
		buf.WriteString(fmt.Sprintf("%s-", s))
	}

	return fmt.Sprintf("%d", HashCode(buf.String()))
}

func IsTest() bool {
	return flag.Lookup("test.v") != nil
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
