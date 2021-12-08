package server

type Server interface {
	ListenAndServe() error
}
