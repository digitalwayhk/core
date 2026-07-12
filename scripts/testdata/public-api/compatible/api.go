package compatible

func Stable(value string) string { return value }

func Removed() {}

func Added() {}

type Service interface {
	Run() error
}
