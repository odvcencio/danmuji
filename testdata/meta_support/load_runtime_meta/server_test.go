package loadruntimemeta_test

import (
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
)

var loadRequestCount atomic.Int64

func TestMain(m *testing.M) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		loadRequestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:18084")
	if err != nil {
		panic(err)
	}

	srv := &http.Server{Handler: mux}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()

	code := m.Run()

	_ = srv.Close()
	<-done
	os.Exit(code)
}
