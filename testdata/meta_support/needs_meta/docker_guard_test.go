package needsmeta_test

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if !dockerAvailable() {
		fmt.Fprintln(os.Stdout, "skipping needs_meta: docker daemon unavailable")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func dockerAvailable() bool {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}

	switch {
	case strings.HasPrefix(host, "unix://"):
		path := strings.TrimPrefix(host, "unix://")
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		u, err := url.Parse(host)
		if err != nil {
			return false
		}
		conn, err := net.DialTimeout("tcp", u.Host, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	default:
		conn, err := net.DialTimeout("unix", host, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
}
