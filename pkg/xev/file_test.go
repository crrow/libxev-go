/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package xev

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crrow/libxev-go/pkg/cxev"
)

func TestFileWriteRead(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	loop, err := NewLoopWithThreadPool()
	if err != nil {
		t.Fatalf("NewLoopWithThreadPool failed: %v", err)
	}
	defer loop.Close()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	file, err := OpenFile(testFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer file.Cleanup()

	testData := []byte("hello libxev file api")
	writeDone := false
	readDone := false
	readData := ""

	err = file.WriteFunc(loop, testData, func(f *File, bytesWritten int, err error) Action {
		if err != nil {
			t.Errorf("write error: %v", err)
			return Stop
		}
		if bytesWritten != len(testData) {
			t.Errorf("expected %d bytes written, got %d", len(testData), bytesWritten)
		}
		writeDone = true
		return Stop
	})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	for i := 0; i < 100 && !writeDone; i++ {
		loop.RunOnce()
	}

	if !writeDone {
		t.Fatal("write did not complete")
	}

	file.CloseFunc(loop, nil)
	for i := 0; i < 100; i++ {
		loop.RunOnce()
	}
	file.Cleanup()

	file2, err := OpenFile(testFile, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile for read failed: %v", err)
	}
	defer file2.Cleanup()

	readBuf := make([]byte, 1024)
	err = file2.ReadFunc(loop, readBuf, func(f *File, data []byte, err error) Action {
		if err != nil {
			t.Errorf("read error: %v", err)
			return Stop
		}
		readData = string(data)
		readDone = true
		return Stop
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	for i := 0; i < 100 && !readDone; i++ {
		loop.RunOnce()
	}

	if !readDone {
		t.Fatal("read did not complete")
	}
	if readData != string(testData) {
		t.Errorf("expected %q, got %q", string(testData), readData)
	}
}

func TestFilePReadPWrite(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	loop, err := NewLoopWithThreadPool()
	if err != nil {
		t.Fatalf("NewLoopWithThreadPool failed: %v", err)
	}
	defer loop.Close()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "ptest.txt")

	file, err := OpenFile(testFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer file.Cleanup()

	write1Done := false
	write2Done := false
	readDone := false
	readData := ""

	err = file.PWriteFunc(loop, []byte("AAAAA"), 0, func(f *File, bytesWritten int, err error) Action {
		if err != nil {
			t.Errorf("pwrite1 error: %v", err)
		}
		write1Done = true
		return Stop
	})
	if err != nil {
		t.Fatalf("PWrite1 failed: %v", err)
	}

	for i := 0; i < 100 && !write1Done; i++ {
		loop.RunOnce()
	}

	err = file.PWriteFunc(loop, []byte("BBBBB"), 5, func(f *File, bytesWritten int, err error) Action {
		if err != nil {
			t.Errorf("pwrite2 error: %v", err)
		}
		write2Done = true
		return Stop
	})
	if err != nil {
		t.Fatalf("PWrite2 failed: %v", err)
	}

	for i := 0; i < 100 && !write2Done; i++ {
		loop.RunOnce()
	}

	readBuf := make([]byte, 5)
	err = file.PReadFunc(loop, readBuf, 5, func(f *File, data []byte, err error) Action {
		if err != nil {
			t.Errorf("pread error: %v", err)
			return Stop
		}
		readData = string(data)
		readDone = true
		return Stop
	})
	if err != nil {
		t.Fatalf("PRead failed: %v", err)
	}

	for i := 0; i < 100 && !readDone; i++ {
		loop.RunOnce()
	}

	if !readDone {
		t.Fatal("pread did not complete")
	}
	if readData != "BBBBB" {
		t.Errorf("expected 'BBBBB', got %q", readData)
	}
}

func TestFileFromFd(t *testing.T) {
	if !cxev.ExtLibLoaded() {
		t.Skip("extended library not loaded")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "fdtest.txt")

	osFile, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("os.Create failed: %v", err)
	}

	fd := int32(osFile.Fd())

	file, err := NewFileFromFd(fd)
	if err != nil {
		t.Fatalf("NewFileFromFd failed: %v", err)
	}

	if file.Fd() != fd {
		t.Errorf("expected fd %d, got %d", fd, file.Fd())
	}

	osFile.Close()
}
