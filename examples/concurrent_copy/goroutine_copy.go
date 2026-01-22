/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// GoroutineCopier copies multiple files concurrently using goroutines.
// Each file copy runs in its own goroutine with blocking I/O.
type GoroutineCopier struct {
	maxWorkers int
}

// NewGoroutineCopier creates a copier with specified concurrency limit.
// If maxWorkers <= 0, uses unlimited concurrency.
func NewGoroutineCopier(maxWorkers int) *GoroutineCopier {
	return &GoroutineCopier{maxWorkers: maxWorkers}
}

// CopyFiles copies all src files to dst paths concurrently.
func (c *GoroutineCopier) CopyFiles(pairs []FilePair) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(pairs))

	if c.maxWorkers > 0 {
		// Use semaphore for concurrency limit
		sem := make(chan struct{}, c.maxWorkers)
		for _, pair := range pairs {
			wg.Add(1)
			go func(src, dst string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				if err := copyFile(src, dst); err != nil {
					errCh <- fmt.Errorf("%s -> %s: %w", src, dst, err)
				}
			}(pair.Src, pair.Dst)
		}
	} else {
		// Unlimited concurrency
		for _, pair := range pairs {
			wg.Add(1)
			go func(src, dst string) {
				defer wg.Done()
				if err := copyFile(src, dst); err != nil {
					errCh <- fmt.Errorf("%s -> %s: %w", src, dst, err)
				}
			}(pair.Src, pair.Dst)
		}
	}

	wg.Wait()
	close(errCh)

	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("%d errors occurred, first: %w", len(errors), errors[0])
	}
	return nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	buf := make([]byte, chunkSize)
	_, err = io.CopyBuffer(dst, src, buf)
	if err != nil {
		return err
	}

	return dst.Sync()
}
