package registry

type Service interface {
	GetManifest()
	PutManifest()

	GetBlob()
	NewBlobCache()
	UpdateBlob()
	FinishBlob()
}