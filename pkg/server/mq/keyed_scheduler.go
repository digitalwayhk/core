package mq

type reliableKeyedWork struct {
	key string
	id  string
	run func() error
}

type reliableKeyedFailure struct {
	work reliableKeyedWork
	err  error
}
