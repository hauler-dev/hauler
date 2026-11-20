package server

import (
	"context"
	"hauler.dev/go/hauler/v2/internal/flags"
	"net/http"
	"testing"
)

func TestFileBindHost(t *testing.T) {
	for _, test := range []struct{ host, want string }{
		{"127.0.0.1", "127.0.0.1:8081"},
		{"::1", "[::1]:8081"},
		{"", ":8081"},
	} {
		server, err := NewFile(context.Background(), flags.ServeFilesOpts{Host: test.host, Port: 8081, RootDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if got := server.(*http.Server).Addr; got != test.want {
			t.Fatalf("got %s, want %s", got, test.want)
		}
	}
}
