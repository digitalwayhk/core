package utils

import (
	"io"
	"os"
	"path/filepath"
)

var TESTPATH string = ""

//Getpath 获取当前路径,当为测试运行时，使用TESTPATH
func Getpath() string {
	if IsTest() {
		return TESTPATH
	}
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return filepath.Dir(ex)
}

//IsExista 判断文件是否存在
func IsExista(file string) bool {
	_, err := os.Lstat(file)
	return !os.IsNotExist(err)
}

//IsDir 判断是否为目录
func IsDir(path string) bool {
	s, err := os.Stat(path)
	if err != nil {
		return false
	}
	return s.IsDir()
}

//IsFile 判断是否为文件
func IsFile(path string) bool {
	return !IsDir(path)
}

//CreateDir 在当前运行路径创建指定名称目录，当测试时，目录的创建路径为TESTPATH
func CreateDir(name string) (string, error) {
	path := Getpath()
	folderPath := filepath.Join(path, name)
	if IsDir(folderPath) {
		return folderPath, nil
	}
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		err = os.MkdirAll(folderPath, 0777)
		if err != nil {
			return "", err
		}
		err = os.Chmod(folderPath, 0777)
		if err != nil {
			return "", err
		}
	}
	return folderPath, nil
}
func ReadFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	chunks := make([]byte, 0)
	buf := make([]byte, 1024)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err != nil && err != io.EOF {
				return "", err
			}
		}
		if n == 0 {
			break
		}
		chunks = append(chunks, buf[:n]...)
	}
	return string(chunks), nil
}

func DeleteFile(file string) error {
	return os.Remove(file)
}

