# Troubleshooting

## SIGSEGV: Segmentation Violation

### Symptom

```
SIGSEGV: segmentation violation
PC=0x187475464 m=9 sigcode=2 addr=0x100
```

The crash occurs when calling FFI functions, particularly `LoopInitWithOptions`.

### Common Causes

#### 1. Struct Layout Mismatch Between Go and Zig

**Indicators:**
- Crash address is a small offset like `0x100` (256), `0x80` (128)
- Crash happens in memory allocator (`libsystem_malloc.dylib`)
- Struct fields contain garbage values

**Why it happens:**
Zig automatically reorders struct fields by alignment (pointers before integers), but Go keeps fields in declaration order. If your Go struct doesn't match Zig's actual memory layout, FFI passes incorrect data.

**Solution:**
1. Check field offsets in both languages
2. Reorder Go struct fields to match Zig's alignment-based order
3. See [FFI and Memory Layout](./ffi-memory-layout.md) for details

**Example fix:**
```go
// Before (WRONG)
type LoopOptions struct {
    Entries    uint32      // offset 0
    _          uint32
    ThreadPool *ThreadPool // offset 8
}

// After (CORRECT)
type LoopOptions struct {
    ThreadPool *ThreadPool // offset 0 (Zig puts pointers first)
    Entries    uint32      // offset 8
    _          uint32
}
```

#### 2. Incorrect Struct Sizes

**Indicators:**
- `@sizeOf(Type)` in Zig doesn't match `unsafe.Sizeof(Type{})` in Go
- Random crashes at different locations

**Solution:**
Add size verification functions and tests:

```zig
// In Zig
export fn xev_loop_sizeof() usize {
    return @sizeOf(xev.Loop);
}
```

```go
// In Go test
func TestSizes(t *testing.T) {
    zigSize := cxev.LoopSizeof()
    goSize := unsafe.Sizeof(cxev.Loop{})
    if zigSize != goSize {
        t.Errorf("size mismatch: zig=%d go=%d", zigSize, goSize)
    }
}
```

#### 3. FFI Calling Convention Issues

**Indicators:**
- Crash before any code in the called function executes
- Works in some contexts but not others

**Solution:**
Ensure FFI function signatures match exactly:
```go
// C signature: int xev_loop_init_with_options(xev_loop* loop, xev_options* options)
fnLoopInitWithOptions, err = lib.Prep("xev_loop_init_with_options",
    &ffi.TypeSint32,    // return type: int -> sint32
    &ffi.TypePointer,   // arg1: xev_loop* -> pointer
    &ffi.TypePointer)   // arg2: xev_options* -> pointer
```

## Extended Library Not Loaded

### Symptom

```
Test skipped: extended library not loaded
```

### Cause

The extended library (`libxev_extended.dylib`) is not found at runtime.

### Solution

Set the `LIBXEV_EXT_PATH` environment variable:

```bash
# macOS
export LIBXEV_EXT_PATH=/path/to/libxev-go/zig/zig-out/lib/libxev_extended.dylib

# Linux
export LIBXEV_EXT_PATH=/path/to/libxev-go/zig/zig-out/lib/libxev_extended.so

# Run tests
go test ./...
```

Or use the justfile:
```bash
just test  # Automatically sets library paths
```

## Thread Pool Operations Fail

### Symptom

File operations don't complete, or callbacks never fire.

### Cause

The loop was initialized without a thread pool, but file operations require a thread pool on kqueue/epoll backends.

### Solution

Use `NewLoopWithThreadPool()` instead of `NewLoop()`:

```go
// Wrong - no thread pool
loop, err := xev.NewLoop()

// Correct - with thread pool for file ops
loop, err := xev.NewLoopWithThreadPool()
if err != nil {
    return err
}
defer loop.Close()
```

## Completion Pointer Issues (Historical)

### Symptom (Before Fix)

SIGSEGV when file operation callbacks are invoked, particularly at `addr=0x100` offset from NULL.

### Historical Cause

libxev's thread pool operations don't preserve extended completion fields. The callback pointer stored in the completion struct was lost when operations went through the thread pool.

### Solution (Implemented)

The file_api.zig now uses heap-allocated context:

```zig
const CallbackContext = extern struct {
    callback: *const anyopaque,
    userdata: ?*anyopaque,
};

// Allocate context on heap, not in completion
const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

// Pass context as userdata
f.write(loop, c, .{ .slice = buf[0..buf_len] }, CallbackContext, ctx, writeCallback);
```

This ensures the callback pointer survives the thread pool transition.

## Debugging Tips

### Enable Debug Output

Add debug prints in Zig code:

```zig
const std = @import("std");

export fn xev_loop_init_with_options(loop: *xev.Loop, options: *const xev.Options) c_int {
    std.debug.print("[DEBUG] entries: {}, thread_pool: {?}\n", .{options.entries, options.thread_pool});
    // ... rest of function
}
```

Rebuild and run tests to see debug output.

### Check Raw Memory

Dump raw bytes to verify layout:

```zig
const bytes: [*]const u8 = @ptrCast(options);
std.debug.print("Raw bytes: ", .{});
for (0..16) |i| {
    std.debug.print("{x:0>2} ", .{bytes[i]});
}
std.debug.print("\n", .{});
```

### Isolate the Issue

Create minimal test programs:

```go
func TestMinimal(t *testing.T) {
    // Test just the failing component
    var opts cxev.LoopOptions
    opts.ThreadPool = &pool
    opts.Entries = 256

    fmt.Printf("Go layout: TP offset=%d, Entries offset=%d\n",
        unsafe.Offsetof(opts.ThreadPool),
        unsafe.Offsetof(opts.Entries))

    // Call and observe crash location
    err := cxev.LoopInitWithOptions(&loop, &opts)
    if err != nil {
        t.Fatal(err)
    }
}
```

## Getting Help

If you encounter issues not covered here:

1. Check the [FFI and Memory Layout](./ffi-memory-layout.md) guide
2. Look at recent commits for similar fixes
3. Create a minimal reproduction case
4. Open an issue with:
   - Go version
   - OS and architecture
   - Full error output including stack trace
   - Code snippet showing the problem
