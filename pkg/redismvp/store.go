/*
 * MIT License
 * Copyright (c) 2026 Crrow
 */

package redismvp

import (
	"errors"
	"strconv"
	"sync"
)

var errValueNotInteger = errors.New("value is not an integer or out of range")

// Store provides thread-safe in-memory key/value storage.
type Store struct {
	mu sync.RWMutex
	kv map[string][]byte
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{kv: make(map[string][]byte)}
}

// Get returns value for key.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	v, ok := s.kv[key]
	s.mu.RUnlock()
	return v, ok
}

// Set stores value for key.
func (s *Store) Set(key string, value []byte) {
	s.mu.Lock()
	s.kv[key] = value
	s.mu.Unlock()
}

// Del deletes keys and returns number of removed keys.
func (s *Store) Del(keys ...string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := int64(0)
	for _, key := range keys {
		if _, ok := s.kv[key]; ok {
			delete(s.kv, key)
			deleted++
		}
	}
	return deleted
}

// Incr increments integer value at key and returns new value.
func (s *Store) Incr(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, ok := s.kv[key]
	if !ok {
		s.kv[key] = []byte("1")
		return 1, nil
	}

	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, errValueNotInteger
	}
	n++
	s.kv[key] = []byte(strconv.FormatInt(n, 10))
	return n, nil
}
