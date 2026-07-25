package old

func Stable(value string) string { return value }

func Removed() {}

type Service interface {
	Run() error
}
