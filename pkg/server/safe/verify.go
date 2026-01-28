package safe

// type CodeInfo struct {
// 	LastTime   time.Time `json:"lastTime"`
// 	Code       string    `json:"code"`
// 	VerifyTime int       `json:"verified"`
// }

// var sessionInfo = map[string]*CodeInfo{}

// const otpChars = "1234567890"

// func GenerateOTP(uname string) string {
// 	redis := &adapter.CacheAdapter{DbName: "mains"}
// 	setCodeInfo := getCachedCode(redis, uname)
// 	if setCodeInfo != nil {
// 		if time.Now().Sub(setCodeInfo.LastTime) < 10*time.Minute {
// 			return ""
// 		}
// 	}

// 	buffer := make([]byte, 6)
// 	_, err := rand.Read(buffer)
// 	if err != nil {
// 		return ""
// 	}
// 	otpCharsLength := len(otpChars)
// 	for i := 0; i < 6; i++ {
// 		buffer[i] = otpChars[int(buffer[i])%otpCharsLength]
// 	}
// 	code := string(buffer)
// 	b, err := json.Marshal(CodeInfo{time.Now(), code, 0})
// 	if err != nil {
// 		log.Println("Redis marshal error: ", err)
// 		return ""
// 	}
// 	err = redis.Set(config.OTPKey+uname, b, 60*10)
// 	if err != nil {
// 		log.Println("Redis set failed: ", err)
// 		return ""
// 	}
// 	return code
// }

// func VerifyCode(uname, code string) error {
// 	redis := &adapter.CacheAdapter{DbName: "mains"}
// 	setCodeInfo := getCachedCode(redis, uname)
// 	if setCodeInfo == nil {
// 		return errors.New(config.NeedVerifyCode)
// 	}
// 	setCodeInfo.VerifyTime = setCodeInfo.VerifyTime + 1

// 	if time.Now().Sub(setCodeInfo.LastTime) > 10*time.Minute {
// 		return errors.New(config.ExpireCode)
// 	}

// 	if setCodeInfo.Code != code {
// 		b, _ := json.Marshal(setCodeInfo)
// 		err := redis.Set(config.OTPKey+uname, b, 60*10)
// 		if err != nil {
// 			return err
// 		}

// 		balanceTime := 3 - setCodeInfo.VerifyTime
// 		if balanceTime > 0 {
// 			return errors.New(fmt.Sprintf(config.InvalidCode, balanceTime))
// 		} else {
// 			return errors.New(config.LockVerifyCode)
// 		}
// 	}

// 	return nil
// }
