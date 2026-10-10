// Package surface pins the consumer-facing compile-time surface of hauler's
// exported packages. It exists so that a change to any pinned symbol's shape
// fails the build here rather than silently breaking the downstream consumer
// that imports hauler as an in-process library.
//
// The package intentionally contains no non-test code beyond this doc comment;
// the guard itself lives in surface_test.go. A doc.go is present so that
// `go build ./...` has a buildable package even though the guard is test-only.
package surface
