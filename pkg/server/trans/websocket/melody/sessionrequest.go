package melody

import (
	"errors"
	"time"
)

type SessionRequest struct {
	ApiKey    string `json:"apiKey"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	Token     string `json:"token"`
	userID    string `json:"userId"`
	userName  string `json:"userName"`
}

func (own *SessionRequest) Response() *SessionResponse {
	key := own.ApiKey
	if key == "" {
		key = own.Token
	}
	return &SessionResponse{
		ApiKey:           key,
		AuthorizedSince:  time.Now().Unix(),
		ConnectedSince:   time.Now().Unix(),
		ReturnRateLimits: false,
	}
}
func (own *SessionRequest) Validate() error {
	if own == nil {
		return errors.New("invalid session request")
	}
	if own.Token == "" && (own.ApiKey == "" || own.Signature == "" || own.Timestamp == 0) {
		return errors.New("invalid session request")
	}
	return nil
}

type SessionResponse struct {
	ApiKey           string `json:"apiKey"`
	AuthorizedSince  int64  `json:"authorizedSince"`
	ConnectedSince   int64  `json:"connectedSince"`
	ReturnRateLimits bool   `json:"returnRateLimits"`
	ServerTime       int64  `json:"serverTime"`
}
