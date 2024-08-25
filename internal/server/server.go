package server

type Server interface {
	ListenAndServe() error
	ListenAndServeTLS(string, string) error
}
