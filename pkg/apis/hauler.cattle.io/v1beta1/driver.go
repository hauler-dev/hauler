package v1beta1

type Driver interface {
	Name() string
	Images() ([]string, error)
}
