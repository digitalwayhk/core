package twosteps

import (
	"io"
	"os"
	"testing"
)

func TestVerifyCodeDoesNotWriteSensitiveValuesToStdout(t *testing.T) {
	secret := NewGoogleAuth().GetSecret()
	code, err := NewGoogleAuth().GetCode(secret)
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout 管道失败: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	valid, verifyErr := NewGoogleAuth().VerifyCode(secret, code)
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭 stdout 写端失败: %v", closeErr)
	}
	os.Stdout = original
	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("读取 stdout 失败: %v", readErr)
	}
	if verifyErr != nil || !valid {
		t.Fatalf("动态码校验失败: valid=%v err=%v", valid, verifyErr)
	}
	if len(output) != 0 {
		t.Fatalf("VerifyCode 不应写 stdout，实际输出 %q", output)
	}
}
