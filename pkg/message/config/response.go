package config

const (
	Success        string = "success"
	Fail                  = "internal failure"
	NeedVerifyCode        = "please enter the verification code"
	InvalidCode           = "incorrect verification code, you still have %v chance(s)"
	LockVerifyCode        = "you conduct the verification too frequent, we will lock it for 10mins"
	ExpireCode            = "you verification code is expired, please enter the new one"
)
