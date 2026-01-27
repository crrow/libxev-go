// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

// Extended C API for libxev File operations.
//
// This file exports File functionality that is available in libxev's Zig API
// but not in the official C API. It follows the same patterns as c_api.zig
// in the upstream libxev project.
//
// File operations typically run on the event loop's thread pool rather than
// core async OS APIs, because most OSes don't support async operations on
// regular files reliably.

const std = @import("std");
const builtin = @import("builtin");
const xev = @import("xev");

// Debug logging
fn debugLog(comptime fmt: []const u8, args: anytype) void {
    std.debug.print("[file_api] " ++ fmt ++ "\n", args);
}

const func_callconv: std.builtin.CallingConvention = if (blk: {
    const order = builtin.zig_version.order(.{ .major = 0, .minor = 14, .patch = 1 });
    break :blk order == .lt or order == .eq;
}) .C else .c;

//-------------------------------------------------------------------
// Types and Constants

pub const XEV_SIZEOF_FILE = 16;

// Context structure to hold Go callback and userdata
// This is created by Go and passed as userdata to libxev
const CallbackContext = extern struct {
    callback: *const anyopaque,
    userdata: ?*anyopaque,
};

pub const xev_file = extern struct {
    data: [XEV_SIZEOF_FILE]u8 align(@alignOf(usize)),
};

pub const xev_file_read_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    [*]u8,
    c_int,
    c_int,
    ?*anyopaque,
) callconv(func_callconv) xev.CallbackAction;

pub const xev_file_write_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int,
    c_int,
    ?*anyopaque,
) callconv(func_callconv) xev.CallbackAction;

pub const xev_file_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int,
    ?*anyopaque,
) callconv(func_callconv) xev.CallbackAction;

//-------------------------------------------------------------------
// File Functions

export fn xev_file_init_fd(file: *xev_file, fd: std.posix.fd_t) void {
    @memset(&file.data, 0);
    const fd_bytes: *[@sizeOf(std.posix.fd_t)]u8 = @ptrCast(&file.data);
    fd_bytes.* = @bitCast(fd);
}

export fn xev_file_fd(file: *const xev_file) std.posix.fd_t {
    return getFd(file);
}

export fn xev_file_read(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]u8,
    buf_len: usize,
    cb: xev_file_read_cb,
    userdata: ?*anyopaque,
) void {
    const f = xev.File.initFd(getFd(file));

    const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
    ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

    // Set threadpool flag for file operations
    if (@hasField(@TypeOf(c.*), "flags")) {
        if (@hasField(@TypeOf(c.flags), "threadpool")) {
            c.flags.threadpool = true;
        }
    }

    debugLog("xev_file_read: c={*}, threadpool={}", .{ c, if (@hasField(@TypeOf(c.*), "flags") and @hasField(@TypeOf(c.flags), "threadpool")) c.flags.threadpool else false });

    f.read(loop, c, .{ .slice = buf[0..buf_len] }, CallbackContext, ctx, (struct {
        fn callback(
            ctx_ptr: ?*CallbackContext,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            buffer: xev.ReadBuffer,
            r: xev.ReadError!usize,
        ) xev.CallbackAction {
            debugLog("read callback: ctx_ptr={?}, cb_c={*}", .{ ctx_ptr, cb_c });

            const context = ctx_ptr orelse return .disarm;
            const cb_fn: xev_file_read_cb = @ptrCast(@alignCast(context.callback));
            const ud = context.userdata;

            const buf_ptr: [*]u8 = switch (buffer) {
                .slice => |s| s.ptr,
                .array => |*a| @constCast(a),
            };

            const action = if (r) |bytes_read| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                buf_ptr,
                @as(c_int, @intCast(bytes_read)),
                @as(c_int, 0),
                ud,
            }) else |err| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                buf_ptr,
                @as(c_int, -1),
                errorCode(err),
                ud,
            });

            if (action != .rearm) {
                std.heap.c_allocator.destroy(context);
            }
            return action;
        }
    }).callback);
}

export fn xev_file_write(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]const u8,
    buf_len: usize,
    cb: xev_file_write_cb,
    userdata: ?*anyopaque,
) void {
    const f = xev.File.initFd(getFd(file));

    const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
    ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

    // Set threadpool flag for file operations on backends that need it
    if (@hasField(@TypeOf(c.*), "flags")) {
        if (@hasField(@TypeOf(c.flags), "threadpool")) {
            c.flags.threadpool = true;
        }
    }

    debugLog("xev_file_write: c={*}, threadpool={}", .{ c, if (@hasField(@TypeOf(c.*), "flags") and @hasField(@TypeOf(c.flags), "threadpool")) c.flags.threadpool else false });
    debugLog("  ctx={*}, ctx.callback={*}, ctx.userdata={?}", .{ ctx, ctx.callback, ctx.userdata });

    f.write(loop, c, .{ .slice = buf[0..buf_len] }, CallbackContext, ctx, (struct {
        fn callback(
            ctx_ptr: ?*CallbackContext,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            _: xev.WriteBuffer,
            r: xev.WriteError!usize,
        ) xev.CallbackAction {
            std.debug.print("WRITE CALLBACK ENTERED!\n", .{});
            debugLog("write callback: ctx_ptr={?}, cb_c={*}", .{ ctx_ptr, cb_c });

            const context = ctx_ptr orelse return .disarm;
            const cb_fn: xev_file_write_cb = @ptrCast(@alignCast(context.callback));
            const ud = context.userdata;

            const action = if (r) |bytes_written| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, @intCast(bytes_written)),
                @as(c_int, 0),
                ud,
            }) else |err| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, -1),
                errorCode(err),
                ud,
            });

            if (action != .rearm) {
                std.heap.c_allocator.destroy(context);
            }
            return action;
        }
    }).callback);
}

export fn xev_file_pread(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]u8,
    buf_len: usize,
    offset: u64,
    cb: xev_file_read_cb,
    userdata: ?*anyopaque,
) void {
    const f = xev.File.initFd(getFd(file));

    const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
    ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

    // Set threadpool flag for file operations
    if (@hasField(@TypeOf(c.*), "flags")) {
        if (@hasField(@TypeOf(c.flags), "threadpool")) {
            c.flags.threadpool = true;
        }
    }

    f.pread(loop, c, .{ .slice = buf[0..buf_len] }, offset, CallbackContext, ctx, (struct {
        fn callback(
            ctx_ptr: ?*CallbackContext,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            buffer: xev.ReadBuffer,
            r: xev.ReadError!usize,
        ) xev.CallbackAction {
            const context = ctx_ptr orelse return .disarm;
            const cb_fn: xev_file_read_cb = @ptrCast(@alignCast(context.callback));
            const ud = context.userdata;

            const buf_ptr: [*]u8 = switch (buffer) {
                .slice => |s| s.ptr,
                .array => |*a| @constCast(a),
            };

            const action = if (r) |bytes_read| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                buf_ptr,
                @as(c_int, @intCast(bytes_read)),
                @as(c_int, 0),
                ud,
            }) else |err| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                buf_ptr,
                @as(c_int, -1),
                errorCode(err),
                ud,
            });

            if (action != .rearm) {
                std.heap.c_allocator.destroy(context);
            }
            return action;
        }
    }).callback);
}

export fn xev_file_pwrite(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]const u8,
    buf_len: usize,
    offset: u64,
    cb: xev_file_write_cb,
    userdata: ?*anyopaque,
) void {
    const f = xev.File.initFd(getFd(file));

    const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
    ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

    // Set threadpool flag for file operations
    if (@hasField(@TypeOf(c.*), "flags")) {
        if (@hasField(@TypeOf(c.flags), "threadpool")) {
            c.flags.threadpool = true;
        }
    }

    f.pwrite(loop, c, .{ .slice = buf[0..buf_len] }, offset, CallbackContext, ctx, (struct {
        fn callback(
            ctx_ptr: ?*CallbackContext,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            _: xev.WriteBuffer,
            r: xev.WriteError!usize,
        ) xev.CallbackAction {
            const context = ctx_ptr orelse return .disarm;
            const cb_fn: xev_file_write_cb = @ptrCast(@alignCast(context.callback));
            const ud = context.userdata;

            const action = if (r) |bytes_written| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, @intCast(bytes_written)),
                @as(c_int, 0),
                ud,
            }) else |err| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, -1),
                errorCode(err),
                ud,
            });

            if (action != .rearm) {
                std.heap.c_allocator.destroy(context);
            }
            return action;
        }
    }).callback);
}

export fn xev_file_close(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    cb: xev_file_cb,
    userdata: ?*anyopaque,
) void {
    const f = xev.File.initFd(getFd(file));

    const ctx = std.heap.c_allocator.create(CallbackContext) catch @panic("alloc failed");
    ctx.* = .{ .callback = @ptrCast(cb), .userdata = userdata };

    // Set threadpool flag for file operations
    if (@hasField(@TypeOf(c.*), "flags")) {
        if (@hasField(@TypeOf(c.flags), "threadpool")) {
            c.flags.threadpool = true;
        }
    }

    f.close(loop, c, CallbackContext, ctx, (struct {
        fn callback(
            ctx_ptr: ?*CallbackContext,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            r: xev.CloseError!void,
        ) xev.CallbackAction {
            const context = ctx_ptr orelse return .disarm;
            const cb_fn: xev_file_cb = @ptrCast(@alignCast(context.callback));
            const ud = context.userdata;

            const action = if (r) |_| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, 0),
                ud,
            }) else |_| @call(.auto, cb_fn, .{
                cb_loop,
                cb_c,
                @as(c_int, -1),
                ud,
            });

            if (action != .rearm) {
                std.heap.c_allocator.destroy(context);
            }
            return action;
        }
    }).callback);
}

//-------------------------------------------------------------------
// Helper Functions

fn getFd(file: *const xev_file) std.posix.fd_t {
    const ptr = @as(*const std.posix.fd_t, @ptrCast(@alignCast(&file.data)));
    return ptr.*;
}

fn errorCode(err: anyerror) c_int {
    return switch (err) {
        error.NotOpenForReading => 1,
        error.NotOpenForWriting => 2,
        error.AccessDenied => 3,
        error.WouldBlock => 4,
        error.SystemResources => 5,
        error.Unexpected => 6,
        else => 99,
    };
}

export fn xev_completion_sizeof() usize {
    return @sizeOf(xev.Completion);
}

export fn xev_completion_alignof() usize {
    return @alignOf(xev.Completion);
}

export fn xev_completion_userdata_offset() usize {
    return @offsetOf(xev.Completion, "userdata");
}

// Debug exports for size verification
export fn xev_loop_sizeof_actual() usize {
    return @sizeOf(xev.Loop);
}

export fn xev_loop_thread_pool_offset() usize {
    return @offsetOf(xev.Loop, "thread_pool");
}

export fn xev_options_sizeof() usize {
    return @sizeOf(xev.Options);
}
