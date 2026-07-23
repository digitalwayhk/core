package incompatible

func Stable(value int) string { return "" }

type Service interface {
	Run() error
	Stop() error
}
