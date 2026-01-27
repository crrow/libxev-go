# FFI and Memory Layout

## Overview

libxev-go uses FFI (Foreign Function Interface) via the `jupiterrider/ffi` library to call into libxev (written in Zig) without requiring cgo. This approach provides several benefits:

- Pure Go build (no C compiler required)
- Better cross-compilation support
- Smaller binary size
- Better goroutine integration

However, FFI requires careful attention to memory layout and struct alignment between Go and Zig.

## The Zig Field Reordering Issue

### Problem

Zig automatically reorders struct fields by their alignment to minimize padding and ensure optimal memory access. This means **fields are not necessarily laid out in memory in the order they are declared**.

### Example: `xev.Options`

In libxev's Zig code, `Options` is declared as:

```zig
pub const Options = struct {
    entries: u32 = 256,
    thread_pool: ?*xev.ThreadPool = null,
};
```

You might expect the memory layout to be:
```
[entries: 4 bytes][padding: 4 bytes][thread_pool: 8 bytes]
```

But Zig **reorders fields by alignment**, resulting in:
```
[thread_pool: 8 bytes][entries: 4 bytes][padding: 4 bytes]
```

The 8-byte pointer comes first, followed by the 4-byte integer.

### Go Struct Declaration

To match this layout in Go, you **cannot** declare fields in source order:

```go
// WRONG - fields in source order
type LoopOptions struct {
    Entries    uint32      // offset 0
    _          uint32      // padding
    ThreadPool *ThreadPool // offset 8
}
```

Instead, you must declare them in **alignment order** to match Zig's layout:

```go
// CORRECT - fields in alignment order
type LoopOptions struct {
    ThreadPool *ThreadPool // offset 0 (8 bytes)
    Entries    uint32      // offset 8 (4 bytes)
    _          uint32      // padding to 16 bytes
}
```

## Debugging Memory Layout Issues

### Symptoms

When Go and Zig struct layouts don't match, you'll typically see:

1. **SIGSEGV crashes** with suspicious addresses like `0x100` (256) or other small offsets
2. **Garbage values** when printing struct fields (e.g., expecting 256 but seeing 1687552)
3. **Crashes in memory allocators** (`libsystem_malloc.dylib`)

### Diagnostic Approach

1. **Check field offsets in Zig:**
   ```zig
   std.debug.print("offsetof(entries): {}\n", .{@offsetOf(xev.Options, "entries")});
   std.debug.print("offsetof(thread_pool): {}\n", .{@offsetOf(xev.Options, "thread_pool")});
   ```

2. **Check field offsets in Go:**
   ```go
   import "unsafe"

   fmt.Printf("Entries offset: %d\n", unsafe.Offsetof(opts.Entries))
   fmt.Printf("ThreadPool offset: %d\n", unsafe.Offsetof(opts.ThreadPool))
   ```

3. **Dump raw bytes:**
   ```zig
   const bytes: [*]const u8 = @ptrCast(options);
   for (0..16) |i| {
       std.debug.print("{x:0>2} ", .{bytes[i]});
   }
   ```

4. **Compare layouts:** If offsets don't match between Go and Zig, you need to reorder Go fields.

## Best Practices

### 1. Order by Alignment

When creating Go structs that map to Zig structs:

1. **List all fields with their sizes and alignments**
2. **Sort by alignment (descending)**: 8-byte pointers first, then 4-byte ints, etc.
3. **Add explicit padding** to match the total struct size

Example:
```go
type MyStruct struct {
    // 8-byte aligned fields first
    Pointer1 *SomeType
    Pointer2 *AnotherType

    // 4-byte aligned fields
    Count  uint32
    Flags  uint32

    // 2-byte aligned fields
    ShortVal uint16

    // Explicit padding to match Zig struct size
    _ [6]byte
}
```

### 2. Document the Layout

Always add comments explaining:
- The Zig struct being mirrored
- Why fields are in a particular order
- The total size and padding

```go
// LoopOptions matches xev.Options in libxev.
// IMPORTANT: Zig reorders struct fields by alignment!
// Actual memory layout:
//   thread_pool: ?*ThreadPool (8 bytes) at offset 0
//   entries: u32 (4 bytes) at offset 8
//   (4 bytes padding to 16 bytes total)
type LoopOptions struct {
    ThreadPool *ThreadPool
    Entries    uint32
    _          uint32
}
```

### 3. Verify with Tests

Create size verification tests:

```go
func TestLayoutSizes(t *testing.T) {
    // Call Zig function that returns struct size
    zigSize := cxev.GetOptionsSize()
    goSize := unsafe.Sizeof(cxev.LoopOptions{})

    if zigSize != goSize {
        t.Errorf("size mismatch: Zig=%d Go=%d", zigSize, goSize)
    }
}
```

### 4. Export Size/Offset Functions from Zig

Add debug exports to verify layouts:

```zig
export fn xev_options_sizeof() usize {
    return @sizeOf(xev.Options);
}

export fn xev_options_field_offsets(entries_offset: *usize, tp_offset: *usize) void {
    entries_offset.* = @offsetOf(xev.Options, "entries");
    tp_offset.* = @offsetOf(xev.Options, "thread_pool");
}
```

## Common Pitfalls

1. **Assuming declaration order equals memory order** - Zig reorders by alignment
2. **Forgetting to add padding** - Structs may have trailing padding
3. **Not checking on both 32-bit and 64-bit** - Pointer sizes differ
4. **Ignoring warnings** - If the code "works sometimes," there's likely a layout bug

## Tools

- `@offsetOf` in Zig - Get field offset at compile time
- `unsafe.Offsetof` in Go - Get field offset
- `@sizeOf` / `unsafe.Sizeof` - Get struct sizes
- Debug prints with raw byte dumps - Visualize actual memory layout

## Related Issues

For the specific issue that led to this documentation:
- **Issue**: SIGSEGV at addr=0x100 during File operations with thread pool
- **Root cause**: Go's `LoopOptions` fields were in declaration order, but Zig's `Options` had fields reordered by alignment
- **Fix**: Reordered Go struct fields to match Zig's alignment-based layout
- **Commit**: f0e0ea3 "fix: add padding to LoopOptions for proper memory alignment"
