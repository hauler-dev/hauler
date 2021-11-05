package artifact

type Config interface {
	// Raw returns the config bytes
	Raw() ([]byte, error)
}
