/*
 * MIT License
 * Copyright (c) 2023 Mitchell Hashimoto
 * Copyright (c) 2026 Crrow
 */

package cxev

/*
#cgo CFLAGS: -I${SRCDIR}/../../deps/libxev/include
#cgo LDFLAGS: -L${SRCDIR}/../../deps/libxev/zig-out/lib -lxev
#include <xev.h>
*/
import "C"

// Loop is the opaque event loop type.
type Loop C.xev_loop

// Completion represents a completion token for async operations.
type Completion C.xev_completion

// Watcher is a generic watcher type used by timers, async, etc.
type Watcher C.xev_watcher

// RunMode specifies how the event loop should run.
type RunMode C.xev_run_mode_t

const (
	RunNoWait    RunMode = C.XEV_RUN_NO_WAIT
	RunOnce      RunMode = C.XEV_RUN_ONCE
	RunUntilDone RunMode = C.XEV_RUN_UNTIL_DONE
)

// CbAction is the return value from callbacks indicating what to do next.
type CbAction C.xev_cb_action

const (
	Disarm CbAction = C.XEV_DISARM
	Rearm  CbAction = C.XEV_REARM
)

// CompletionState represents the state of a completion.
type CompletionState C.xev_completion_state_t

const (
	CompletionDead   CompletionState = C.XEV_COMPLETION_DEAD
	CompletionActive CompletionState = C.XEV_COMPLETION_ACTIVE
)
