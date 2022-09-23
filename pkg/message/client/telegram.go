package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

var (
	Token = "xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
)

func getUrl() string {
	return fmt.Sprintf("https://api.telegram.org/bot%s", Token)
}

func SendTgMessage(ChatId, text string) (bool, error) {
	// Global variables
	var err error
	var response *http.Response

	// Send the message
	url := fmt.Sprintf("%s/sendMessage", getUrl())
	body, _ := json.Marshal(map[string]string{
		"chat_id": ChatId,
		"text":    text,
	})
	response, err = http.Post(
		url,
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return false, err
	}

	// Close the request at the end
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(response.Body)

	// Body
	body, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return false, err
	}

	fmt.Printf("Message '%s' was sent\n", text)
	fmt.Printf("Response JSON: %s\n", string(body))

	// Return
	return true, nil
}
