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

const func_callconv: std.builtin.CallingConvention = if (blk: {
    const order = builtin.zig_version.order(.{ .major = 0, .minor = 14, .patch = 1 });
    break :blk order == .lt or order == .eq;
}) .C else .c;

//-------------------------------------------------------------------
// Types and Constants

pub const XEV_SIZEOF_FILE = 16;

const Completion = extern struct {
    const Data = [@sizeOf(xev.Completion)]u8;
    data: Data,
    c_callback: *const anyopaque,
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
    userdata: ?*anyopaque,
    cb: xev_file_read_cb,
) void {
    const f = xev.File.initFd(getFd(file));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    f.read(loop, c, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            buffer: xev.ReadBuffer,
            r: xev.ReadError!usize,
        ) xev.CallbackAction {
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            const buf_ptr: [*]u8 = switch (buffer) {
                .slice => |s| s.ptr,
                .array => |*a| @constCast(a),
            };

            if (r) |bytes_read| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    buf_ptr,
                    @as(c_int, @intCast(bytes_read)),
                    @as(c_int, 0),
                    ud,
                });
            } else |err| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    buf_ptr,
                    @as(c_int, -1),
                    errorCode(err),
                    ud,
                });
            }
        }
    }).callback);
}

export fn xev_file_write(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]const u8,
    buf_len: usize,
    userdata: ?*anyopaque,
    cb: xev_file_write_cb,
) void {
    const f = xev.File.initFd(getFd(file));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    f.write(loop, c, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            _: xev.WriteBuffer,
            r: xev.WriteError!usize,
        ) xev.CallbackAction {
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            if (r) |bytes_written| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    @as(c_int, @intCast(bytes_written)),
                    @as(c_int, 0),
                    ud,
                });
            } else |err| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    @as(c_int, -1),
                    errorCode(err),
                    ud,
                });
            }
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
    userdata: ?*anyopaque,
    cb: xev_file_read_cb,
) void {
    const f = xev.File.initFd(getFd(file));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    f.pread(loop, c, .{ .slice = buf[0..buf_len] }, offset, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            buffer: xev.ReadBuffer,
            r: xev.ReadError!usize,
        ) xev.CallbackAction {
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            const buf_ptr: [*]u8 = switch (buffer) {
                .slice => |s| s.ptr,
                .array => |*a| @constCast(a),
            };

            if (r) |bytes_read| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    buf_ptr,
                    @as(c_int, @intCast(bytes_read)),
                    @as(c_int, 0),
                    ud,
                });
            } else |err| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    buf_ptr,
                    @as(c_int, -1),
                    errorCode(err),
                    ud,
                });
            }
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
    userdata: ?*anyopaque,
    cb: xev_file_write_cb,
) void {
    const f = xev.File.initFd(getFd(file));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    f.pwrite(loop, c, .{ .slice = buf[0..buf_len] }, offset, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            _: xev.WriteBuffer,
            r: xev.WriteError!usize,
        ) xev.CallbackAction {
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            if (r) |bytes_written| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    @as(c_int, @intCast(bytes_written)),
                    @as(c_int, 0),
                    ud,
                });
            } else |err| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    @as(c_int, -1),
                    errorCode(err),
                    ud,
                });
            }
        }
    }).callback);
}

export fn xev_file_close(
    file: *xev_file,
    loop: *xev.Loop,
    c: *xev.Completion,
    userdata: ?*anyopaque,
    cb: xev_file_cb,
) void {
    const f = xev.File.initFd(getFd(file));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    f.close(loop, c, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.File,
            r: xev.CloseError!void,
        ) xev.CallbackAction {
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            if (r) |_| {
                return @call(.auto, cb_c_callback, .{ cb_loop, cb_c, @as(c_int, 0), ud });
            } else |err| {
                return @call(.auto, cb_c_callback, .{ cb_loop, cb_c, errorCode(err), ud });
            }
        }
    }).callback);
}

//-------------------------------------------------------------------
// Size Constants

export fn xev_sizeof_file() usize {
    return XEV_SIZEOF_FILE;
}

export fn xev_sizeof_file_completion() usize {
    return @sizeOf(Completion);
}

//-------------------------------------------------------------------
// Internal Helpers

fn getFd(file: *const xev_file) std.posix.fd_t {
    const fd_bytes: *const [@sizeOf(std.posix.fd_t)]u8 = @ptrCast(&file.data);
    return @bitCast(fd_bytes.*);
}

fn errorCode(err: anyerror) c_int {
    return @intFromError(err);
}

//-------------------------------------------------------------------
// Tests

test "file sizes" {
    const testing = std.testing;
    try testing.expect(@sizeOf(std.posix.fd_t) <= XEV_SIZEOF_FILE);
    try testing.expect(@sizeOf(Completion) > @sizeOf(xev.Completion));
}
