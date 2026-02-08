/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/crrow/libxev-go/pkg/rediscli"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6379", "redis server address")
	auth := flag.String("auth", "", "auth token placeholder (not used yet)")
	flag.Parse()

	if *auth != "" {
		_, _ = fmt.Fprintln(os.Stderr, "warning: --auth is currently a placeholder and is not applied")
	}

	client := rediscli.NewClient(*addr)
	exitCode := client.Run(flag.Args(), os.Stdin, os.Stdout, os.Stderr)
	os.Exit(exitCode)
}
