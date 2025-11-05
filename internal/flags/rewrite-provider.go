package flags

// allows both SyncOpts and Add..Opts to return rewrite string for storeImage and other functions
type RewriteProvider interface {
	RewriteValue() string
}
