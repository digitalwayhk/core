package test

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/message/client"
	"testing"
)

func Test_Send(t *testing.T) {
	_, err := client.SendTgMessage("320974596", "TG sending check")
	if err != nil {
		fmt.Println(err)
	}
}
