// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

// Extended C API for libxev UDP operations.
//
// This file exports UDP functionality that is available in libxev's Zig API
// but not in the official C API. It follows the same patterns as c_api.zig
// in the upstream libxev project.
//
// UDP is connectionless - each read/write operation includes the remote address.
// On kqueue (macOS), UDP uses sendto/recvfrom. On Linux (io_uring/epoll), it
// uses sendmsg/recvmsg for better performance.

const std = @import("std");
const builtin = @import("builtin");
const xev = @import("xev");

const func_callconv: std.builtin.CallingConvention = if (blk: {
    const order = builtin.zig_version.order(.{ .major = 0, .minor = 14, .patch = 1 });
    break :blk order == .lt or order == .eq;
}) .C else .c;

//-------------------------------------------------------------------
// Types and Constants

/// Size for UDP socket storage - must be >= sizeof(socket_t)
pub const XEV_SIZEOF_UDP = 16;

/// Size for UDP state storage.
/// UDP operations require extra state for address handling.
pub const XEV_SIZEOF_UDP_STATE = 256;

/// Extended Completion struct with space for C callback pointer.
const Completion = extern struct {
    const Data = [@sizeOf(xev.Completion)]u8;
    data: Data,
    c_callback: *const anyopaque,
};

/// Opaque UDP socket type for C API
pub const xev_udp = extern struct {
    data: [XEV_SIZEOF_UDP]u8 align(@alignOf(usize)),
};

/// UDP state storage for operations.
/// This stores the userdata pointer and operation-specific data.
pub const xev_udp_state = extern struct {
    data: [XEV_SIZEOF_UDP_STATE]u8 align(@alignOf(usize)),
};

// Re-use sockaddr from tcp_api
const tcp_api = @import("tcp_api.zig");
pub const xev_sockaddr = tcp_api.xev_sockaddr;
pub const XEV_SIZEOF_SOCKADDR = tcp_api.XEV_SIZEOF_SOCKADDR;

// Forward declare the exported functions from tcp_api for use in tests
extern fn xev_sockaddr_ipv4(addr: *xev_sockaddr, ip_a: u8, ip_b: u8, ip_c: u8, ip_d: u8, port: u16) void;
extern fn xev_sockaddr_port(addr: *const xev_sockaddr) u16;

/// Callback type for UDP read operations (recvfrom).
/// Includes the remote address from which data was received.
pub const xev_udp_read_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    *xev_sockaddr, // remote address
    [*]u8, // buffer pointer
    c_int, // bytes read or -1 on error
    c_int, // error code (0 on success)
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

/// Callback type for UDP write operations (sendto).
pub const xev_udp_write_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int, // bytes written or -1 on error
    c_int, // error code (0 on success)
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

/// Callback type for close operations.
pub const xev_udp_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int, // result or error code
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

//-------------------------------------------------------------------
// UDP Functions

/// Initialize a UDP socket with the given address family.
/// Returns 0 on success, error code on failure.
export fn xev_udp_init(udp: *xev_udp, family: c_int) c_int {
    const address = if (family == std.posix.AF.INET)
        std.net.Address.initIp4(.{ 0, 0, 0, 0 }, 0)
    else if (family == std.posix.AF.INET6)
        std.net.Address.initIp6(.{ 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 }, 0, 0, 0)
    else
        return -1;

    const socket = xev.UDP.init(address) catch |err| return errorCode(err);

    // Store the socket fd in our opaque type
    @memset(&udp.data, 0);
    const fd_bytes: *[@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&udp.data);
    fd_bytes.* = @bitCast(socket.fd);

    return 0;
}

/// Initialize a UDP socket from an existing file descriptor.
export fn xev_udp_init_fd(udp: *xev_udp, fd: std.posix.socket_t) void {
    @memset(&udp.data, 0);
    const fd_bytes: *[@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&udp.data);
    fd_bytes.* = @bitCast(fd);
}

/// Get the file descriptor from a UDP socket.
export fn xev_udp_fd(udp: *const xev_udp) std.posix.socket_t {
    return getFd(udp);
}

/// Bind a UDP socket to an address.
/// Returns 0 on success, error code on failure.
export fn xev_udp_bind(udp: *xev_udp, addr: *const xev_sockaddr) c_int {
    const socket = xev.UDP.initFd(getFd(udp));
    const address = sockaddrToAddress(addr);

    socket.bind(address) catch |err| return errorCode(err);
    return 0;
}

/// Get the local address of a bound UDP socket.
/// Returns 0 on success, error code on failure.
export fn xev_udp_getsockname(udp: *const xev_udp, addr: *xev_sockaddr) c_int {
    const fd = getFd(udp);
    var sock_addr: std.posix.sockaddr.storage = undefined;
    var sock_len: std.posix.socklen_t = @sizeOf(std.posix.sockaddr.storage);

    std.posix.getsockname(fd, @ptrCast(&sock_addr), &sock_len) catch |err| return errorCode(err);

    @memset(&addr.data, 0);
    const src: [*]const u8 = @ptrCast(&sock_addr);
    @memcpy(addr.data[0..sock_len], src[0..sock_len]);
    return 0;
}

/// Read from a UDP socket (recvfrom).
/// This is an async operation - the callback will be invoked when complete.
/// The callback receives the remote address from which data was received.
export fn xev_udp_read(
    udp: *xev_udp,
    loop: *xev.Loop,
    c: *xev.Completion,
    state: *xev_udp_state,
    buf: [*]u8,
    buf_len: usize,
    userdata: ?*anyopaque,
    cb: xev_udp_read_cb,
) void {
    const socket = xev.UDP.initFd(getFd(udp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    // Get the internal state pointer
    const internal_state: *xev.UDP.State = @ptrCast(@alignCast(&state.data));

    socket.read(loop, c, internal_state, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            cb_state: *xev.UDP.State,
            addr: std.net.Address,
            _: xev.UDP,
            buffer: xev.ReadBuffer,
            r: xev.ReadError!usize,
        ) xev.CallbackAction {
            _ = cb_state;
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            const buf_ptr: [*]u8 = switch (buffer) {
                .slice => |s| s.ptr,
                .array => |*a| @constCast(a),
            };

            // Convert the address to xev_sockaddr
            var remote_addr: xev_sockaddr = undefined;
            @memset(&remote_addr.data, 0);
            if (addr.any.family == std.posix.AF.INET) {
                const src = std.mem.asBytes(&addr.in);
                @memcpy(remote_addr.data[0..src.len], src);
            } else if (addr.any.family == std.posix.AF.INET6) {
                const src = std.mem.asBytes(&addr.in6);
                @memcpy(remote_addr.data[0..src.len], src);
            }

            if (r) |bytes_read| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    &remote_addr,
                    buf_ptr,
                    @as(c_int, @intCast(bytes_read)),
                    @as(c_int, 0),
                    ud,
                });
            } else |err| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    &remote_addr,
                    buf_ptr,
                    @as(c_int, -1),
                    errorCode(err),
                    ud,
                });
            }
        }
    }).callback);
}

/// Write to a UDP socket (sendto).
/// This is an async operation - the callback will be invoked when complete.
export fn xev_udp_write(
    udp: *xev_udp,
    loop: *xev.Loop,
    c: *xev.Completion,
    state: *xev_udp_state,
    addr: *const xev_sockaddr,
    buf: [*]const u8,
    buf_len: usize,
    userdata: ?*anyopaque,
    cb: xev_udp_write_cb,
) void {
    const socket = xev.UDP.initFd(getFd(udp));
    const address = sockaddrToAddress(addr);
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    // Get the internal state pointer
    const internal_state: *xev.UDP.State = @ptrCast(@alignCast(&state.data));

    socket.write(loop, c, internal_state, address, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            cb_state: *xev.UDP.State,
            _: xev.UDP,
            _: xev.WriteBuffer,
            r: xev.WriteError!usize,
        ) xev.CallbackAction {
            _ = cb_state;
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

/// Close a UDP socket.
/// This is an async operation - the callback will be invoked when complete.
export fn xev_udp_close(
    udp: *xev_udp,
    loop: *xev.Loop,
    c: *xev.Completion,
    userdata: ?*anyopaque,
    cb: xev_udp_cb,
) void {
    const socket = xev.UDP.initFd(getFd(udp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.close(loop, c, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.UDP,
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
// Size Constants for Go FFI

export fn xev_sizeof_udp() usize {
    return XEV_SIZEOF_UDP;
}

export fn xev_sizeof_udp_state() usize {
    return XEV_SIZEOF_UDP_STATE;
}

/// Size of extended completion struct (includes space for C callback pointer).
export fn xev_sizeof_udp_completion() usize {
    return @sizeOf(Completion);
}

//-------------------------------------------------------------------
// Internal Helpers

fn getFd(udp: *const xev_udp) std.posix.socket_t {
    const fd_bytes: *const [@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&udp.data);
    return @bitCast(fd_bytes.*);
}

fn sockaddrToAddress(addr: *const xev_sockaddr) std.net.Address {
    const in_ptr: *const std.net.Ip4Address = @ptrCast(@alignCast(&addr.data));
    const family = in_ptr.sa.family;

    if (family == std.posix.AF.INET) {
        return .{ .in = in_ptr.* };
    } else if (family == std.posix.AF.INET6) {
        const in6_ptr: *const std.net.Ip6Address = @ptrCast(@alignCast(&addr.data));
        return .{ .in6 = in6_ptr.* };
    }

    return std.net.Address.initIp4(.{ 0, 0, 0, 0 }, 0);
}

fn errorCode(err: anyerror) c_int {
    return @intFromError(err);
}

//-------------------------------------------------------------------
// Tests

test "udp sizes" {
    const testing = std.testing;

    // Ensure our opaque type is large enough for a socket fd
    try testing.expect(@sizeOf(std.posix.socket_t) <= XEV_SIZEOF_UDP);

    // Ensure state is large enough
    try testing.expect(@sizeOf(xev.UDP.State) <= XEV_SIZEOF_UDP_STATE);

    // Extended completion must be larger than base completion
    try testing.expect(@sizeOf(Completion) > @sizeOf(xev.Completion));
}

test "udp init and bind" {
    const testing = std.testing;

    var udp: xev_udp = undefined;
    const init_result = xev_udp_init(&udp, std.posix.AF.INET);
    try testing.expectEqual(@as(c_int, 0), init_result);

    // Bind to localhost on port 0 (auto-assign)
    var addr: xev_sockaddr = undefined;
    xev_sockaddr_ipv4(&addr, 127, 0, 0, 1, 0);

    const bind_result = xev_udp_bind(&udp, &addr);
    try testing.expectEqual(@as(c_int, 0), bind_result);

    // Get the assigned port
    var bound_addr: xev_sockaddr = undefined;
    const getsockname_result = xev_udp_getsockname(&udp, &bound_addr);
    try testing.expectEqual(@as(c_int, 0), getsockname_result);

    const bound_port = xev_sockaddr_port(&bound_addr);
    try testing.expect(bound_port > 0);

    // Clean up
    std.posix.close(xev_udp_fd(&udp));
}
