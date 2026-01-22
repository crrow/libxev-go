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
	fnLoopSetThreadPool    ffi.Fun
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

	if libExt.Addr != 0 {
		fnLoopSetThreadPool, err = libExt.Prep("xev_loop_set_thread_pool", &ffi.TypeVoid, &ffi.TypePointer, &ffi.TypePointer)
		if err != nil {
			return err
		}
	}

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

func LoopSetThreadPool(loop *Loop, pool *ThreadPool) {
	loopPtr := unsafe.Pointer(loop)
	poolPtr := unsafe.Pointer(pool)
	fnLoopSetThreadPool.Call(nil, &loopPtr, &poolPtr)
}
