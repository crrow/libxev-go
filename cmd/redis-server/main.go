/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/crrow/libxev-go/pkg/redismvp"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6379", "listen address")
	flag.Parse()

	srv, err := redismvp.Start(*addr)
	if err != nil {
		log.Fatalf("start redis server failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	fmt.Printf("redis-server listening on %s\n", srv.Addr())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	if err = srv.Close(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
