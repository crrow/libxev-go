/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

import (
	"unsafe"

	"github.com/jupiterrider/ffi"
)

var (
	fnThreadPoolInit       ffi.Fun
	fnThreadPoolDeinit     ffi.Fun
	fnThreadPoolShutdown   ffi.Fun
	fnThreadPoolConfigInit ffi.Fun
)

func registerThreadPoolFunctions() error {
	var err error

	fnThreadPoolConfigInit, err = lib.Prep("xev_threadpool_config_init", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	fnThreadPoolInit, err = lib.Prep("xev_threadpool_init", &ffi.TypeVoid, &ffi.TypePointer, &ffi.TypePointer)
	if err != nil {
		return err
	}

	fnThreadPoolShutdown, err = lib.Prep("xev_threadpool_shutdown", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	fnThreadPoolDeinit, err = lib.Prep("xev_threadpool_deinit", &ffi.TypeVoid, &ffi.TypePointer)
	if err != nil {
		return err
	}

	// NOTE: xev_loop_set_thread_pool is removed in the new libxev API.
	// Thread pools must now be passed via LoopOptions during initialization.
	// Use LoopInitWithOptions instead.

	return nil
}

func ThreadPoolConfigInit(cfg *ThreadPoolConfig) {
	ptr := unsafe.Pointer(cfg)
	fnThreadPoolConfigInit.Call(nil, &ptr)
}

func ThreadPoolInit(pool *ThreadPool, cfg *ThreadPoolConfig) {
	poolPtr := unsafe.Pointer(pool)
	var cfgPtr unsafe.Pointer
	if cfg != nil {
		cfgPtr = unsafe.Pointer(cfg)
	}
	fnThreadPoolInit.Call(nil, &poolPtr, &cfgPtr)
}

func ThreadPoolDeinit(pool *ThreadPool) {
	ptr := unsafe.Pointer(pool)
	fnThreadPoolDeinit.Call(nil, &ptr)
}

func ThreadPoolShutdown(pool *ThreadPool) {
	ptr := unsafe.Pointer(pool)
	fnThreadPoolShutdown.Call(nil, &ptr)
}

// NOTE: LoopSetThreadPool is deprecated and removed.
// libxev no longer supports setting thread_pool after Loop initialization.
// Use LoopInitWithOptions to pass a thread pool during initialization instead.
//
// Example:
//   var pool ThreadPool
//   ThreadPoolInit(&pool, nil)
//
//   var loop Loop
//   opts := &LoopOptions{
//       Entries: 256,
//       ThreadPool: &pool,
//   }
//   LoopInitWithOptions(&loop, opts)
