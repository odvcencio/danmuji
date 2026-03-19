package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ready := flag.String("ready", "", "stdout readiness marker")
	message := flag.String("message", "", "stdout message")
	flag.Parse()

	if *ready != "" {
		fmt.Fprintln(os.Stdout, *ready)
	}
	if *message != "" {
		fmt.Fprintln(os.Stdout, *message)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigCh
	fmt.Fprintf(os.Stderr, "shutdown complete: %s\n", sig.String())
}
