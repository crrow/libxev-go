// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

// Extended C API for libxev TCP operations.
//
// This file exports TCP functionality that is available in libxev's Zig API
// but not in the official C API. It follows the same patterns as c_api.zig
// in the upstream libxev project.
//
// Key design: C callers must allocate XEV_SIZEOF_TCP_COMPLETION bytes for
// completions (not the smaller xev.Completion size). This extra space stores
// the C callback pointer, following the same pattern as libxev's c_api.zig.

const std = @import("std");
const builtin = @import("builtin");
const xev = @import("xev");

// Calling convention compatible with Zig 0.14+
const func_callconv: std.builtin.CallingConvention = if (blk: {
    const order = builtin.zig_version.order(.{ .major = 0, .minor = 14, .patch = 1 });
    break :blk order == .lt or order == .eq;
}) .C else .c;

//-------------------------------------------------------------------
// Types and Constants

/// Size for TCP socket storage - must be >= sizeof(socket_t)
pub const XEV_SIZEOF_TCP = 16;

/// Size for socket address storage - must be >= sizeof(sockaddr_storage)
pub const XEV_SIZEOF_SOCKADDR = 128;

/// Address family constants - use platform values directly
pub const XEV_AF_INET: c_int = std.posix.AF.INET;
pub const XEV_AF_INET6: c_int = std.posix.AF.INET6;

/// Extended Completion struct with space for C callback pointer.
/// C callers must allocate XEV_SIZEOF_TCP_COMPLETION bytes.
/// This follows the same pattern as libxev's c_api.zig.
const Completion = extern struct {
    const Data = [@sizeOf(xev.Completion)]u8;
    data: Data,
    c_callback: *const anyopaque,
};

/// Opaque TCP socket type for C API
pub const xev_tcp = extern struct {
    data: [XEV_SIZEOF_TCP]u8 align(@alignOf(usize)),
};

/// Socket address wrapper for C API
pub const xev_sockaddr = extern struct {
    data: [XEV_SIZEOF_SOCKADDR]u8 align(@alignOf(usize)),
};

/// Callback type for simple operations (connect, close, shutdown)
pub const xev_tcp_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int, // result or error code
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

/// Callback type for accept - returns accepted fd
pub const xev_tcp_accept_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int, // accepted fd or -1 on error
    c_int, // error code (0 on success)
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

/// Callback type for read operations
pub const xev_tcp_read_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    [*]u8, // buffer pointer
    c_int, // bytes read or -1 on error
    c_int, // error code (0 on success)
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

/// Callback type for write operations
pub const xev_tcp_write_cb = *const fn (
    *xev.Loop,
    *xev.Completion,
    c_int, // bytes written or -1 on error
    c_int, // error code (0 on success)
    ?*anyopaque, // userdata
) callconv(func_callconv) xev.CallbackAction;

//-------------------------------------------------------------------
// Address Helper Functions

/// Initialize a sockaddr for IPv4
export fn xev_sockaddr_ipv4(
    addr: *xev_sockaddr,
    ip_a: u8,
    ip_b: u8,
    ip_c: u8,
    ip_d: u8,
    port: u16,
) void {
    const ip_bytes = [4]u8{ ip_a, ip_b, ip_c, ip_d };
    const ip_addr = std.net.Address.initIp4(ip_bytes, port);

    // Zero the buffer first
    @memset(&addr.data, 0);

    // Copy the sockaddr.in struct
    const src = std.mem.asBytes(&ip_addr.in);
    @memcpy(addr.data[0..src.len], src);
}

/// Initialize a sockaddr for IPv6
export fn xev_sockaddr_ipv6(
    addr: *xev_sockaddr,
    ip: *const [16]u8,
    port: u16,
    flowinfo: u32,
    scope_id: u32,
) void {
    const ip_addr = std.net.Address.initIp6(ip.*, port, flowinfo, scope_id);

    // Zero the buffer first
    @memset(&addr.data, 0);

    // Copy the sockaddr.in6 struct
    const src = std.mem.asBytes(&ip_addr.in6);
    @memcpy(addr.data[0..src.len], src);
}

/// Get the port from a sockaddr
export fn xev_sockaddr_port(addr: *const xev_sockaddr) u16 {
    const net_addr = sockaddrToAddress(addr);
    return net_addr.getPort();
}

fn sockaddrToAddress(addr: *const xev_sockaddr) std.net.Address {
    // Cast directly to Ip4Address to read the family field correctly.
    // On BSD (macOS), sockaddr_in has: sin_len (u8), sin_family (u8), sin_port (u16), sin_addr (u32)
    // Zig's Ip4Address struct matches this layout.
    const in_ptr: *const std.net.Ip4Address = @ptrCast(@alignCast(&addr.data));
    const family = in_ptr.sa.family;

    if (family == std.posix.AF.INET) {
        return .{ .in = in_ptr.* };
    } else if (family == std.posix.AF.INET6) {
        const in6_ptr: *const std.net.Ip6Address = @ptrCast(@alignCast(&addr.data));
        return .{ .in6 = in6_ptr.* };
    }

    // Default to IPv4 zero address
    return std.net.Address.initIp4(.{ 0, 0, 0, 0 }, 0);
}

//-------------------------------------------------------------------
// TCP Functions

/// Initialize a TCP socket with the given address family.
/// Returns 0 on success, error code on failure.
export fn xev_tcp_init(tcp: *xev_tcp, family: c_int) c_int {
    const address = if (family == std.posix.AF.INET)
        std.net.Address.initIp4(.{ 0, 0, 0, 0 }, 0)
    else if (family == std.posix.AF.INET6)
        std.net.Address.initIp6(.{ 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 }, 0, 0, 0)
    else
        return -1;

    const socket = xev.TCP.init(address) catch |err| return errorCode(err);

    // Store the socket fd in our opaque type
    @memset(&tcp.data, 0);
    const fd_bytes: *[@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&tcp.data);
    fd_bytes.* = @bitCast(socket.fd);

    return 0;
}

/// Initialize a TCP socket from an existing file descriptor.
export fn xev_tcp_init_fd(tcp: *xev_tcp, fd: std.posix.socket_t) void {
    @memset(&tcp.data, 0);
    const fd_bytes: *[@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&tcp.data);
    fd_bytes.* = @bitCast(fd);
}

/// Get the file descriptor from a TCP socket.
export fn xev_tcp_fd(tcp: *const xev_tcp) std.posix.socket_t {
    return getFd(tcp);
}

/// Bind a TCP socket to an address.
/// Returns 0 on success, error code on failure.
export fn xev_tcp_bind(tcp: *xev_tcp, addr: *const xev_sockaddr) c_int {
    const socket = xev.TCP.initFd(getFd(tcp));
    const address = sockaddrToAddress(addr);

    socket.bind(address) catch |err| return errorCode(err);
    return 0;
}

/// Start listening on a TCP socket.
/// Returns 0 on success, error code on failure.
export fn xev_tcp_listen(tcp: *xev_tcp, backlog: c_int) c_int {
    const socket = xev.TCP.initFd(getFd(tcp));
    socket.listen(@intCast(backlog)) catch |err| return errorCode(err);
    return 0;
}

/// Get the local address of a bound TCP socket.
/// Returns 0 on success, error code on failure.
export fn xev_tcp_getsockname(tcp: *const xev_tcp, addr: *xev_sockaddr) c_int {
    const fd = getFd(tcp);
    var sock_addr: std.posix.sockaddr.storage = undefined;
    var sock_len: std.posix.socklen_t = @sizeOf(std.posix.sockaddr.storage);

    std.posix.getsockname(fd, @ptrCast(&sock_addr), &sock_len) catch |err| return errorCode(err);

    @memset(&addr.data, 0);
    const src: [*]const u8 = @ptrCast(&sock_addr);
    @memcpy(addr.data[0..sock_len], src[0..sock_len]);
    return 0;
}

/// Accept a connection on a listening socket.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_accept(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    userdata: ?*anyopaque,
    cb: xev_tcp_accept_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.accept(loop, c, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            r: xev.AcceptError!xev.TCP,
        ) xev.CallbackAction {
            // Recover the C callback from extended completion
            const cb_extern_c: *Completion = @ptrCast(@alignCast(cb_c));
            const cb_c_callback: *const Callback = @ptrCast(@alignCast(cb_extern_c.c_callback));

            if (r) |accepted| {
                return @call(.auto, cb_c_callback, .{
                    cb_loop,
                    cb_c,
                    @as(c_int, @intCast(accepted.fd)),
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

/// Connect to a remote address.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_connect(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    addr: *const xev_sockaddr,
    userdata: ?*anyopaque,
    cb: xev_tcp_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const address = sockaddrToAddress(addr);
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.connect(loop, c, address, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.TCP,
            r: xev.ConnectError!void,
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

/// Read from a TCP socket.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_read(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]u8,
    buf_len: usize,
    userdata: ?*anyopaque,
    cb: xev_tcp_read_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.read(loop, c, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.TCP,
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

/// Write to a TCP socket.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_write(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    buf: [*]const u8,
    buf_len: usize,
    userdata: ?*anyopaque,
    cb: xev_tcp_write_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.write(loop, c, .{ .slice = buf[0..buf_len] }, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.TCP,
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

/// Close a TCP socket.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_close(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    userdata: ?*anyopaque,
    cb: xev_tcp_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.close(loop, c, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.TCP,
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

/// Shutdown the write side of a TCP socket.
/// This is an async operation - the callback will be invoked when complete.
/// Note: The completion must be XEV_SIZEOF_TCP_COMPLETION bytes.
export fn xev_tcp_shutdown(
    tcp: *xev_tcp,
    loop: *xev.Loop,
    c: *xev.Completion,
    userdata: ?*anyopaque,
    cb: xev_tcp_cb,
) void {
    const socket = xev.TCP.initFd(getFd(tcp));
    const Callback = @typeInfo(@TypeOf(cb)).pointer.child;

    // Store callback in the extended completion struct
    const extern_c: *Completion = @ptrCast(@alignCast(c));
    extern_c.c_callback = @ptrCast(cb);

    socket.shutdown(loop, c, anyopaque, userdata, (struct {
        fn callback(
            ud: ?*anyopaque,
            cb_loop: *xev.Loop,
            cb_c: *xev.Completion,
            _: xev.TCP,
            r: xev.ShutdownError!void,
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

export fn xev_sizeof_tcp() usize {
    return XEV_SIZEOF_TCP;
}

export fn xev_sizeof_sockaddr() usize {
    return XEV_SIZEOF_SOCKADDR;
}

/// Size of extended completion struct (includes space for C callback pointer).
/// C/Go callers must allocate this many bytes for completions.
export fn xev_sizeof_tcp_completion() usize {
    return @sizeOf(Completion);
}

export fn xev_af_inet() c_int {
    return XEV_AF_INET;
}

export fn xev_af_inet6() c_int {
    return XEV_AF_INET6;
}

//-------------------------------------------------------------------
// Internal Helpers

fn getFd(tcp: *const xev_tcp) std.posix.socket_t {
    const fd_bytes: *const [@sizeOf(std.posix.socket_t)]u8 = @ptrCast(&tcp.data);
    return @bitCast(fd_bytes.*);
}

/// Returns the unique error code for an error.
fn errorCode(err: anyerror) c_int {
    return @intFromError(err);
}

//-------------------------------------------------------------------
// Tests

test "tcp sizes" {
    const testing = std.testing;

    // Ensure our opaque type is large enough for a socket fd
    try testing.expect(@sizeOf(std.posix.socket_t) <= XEV_SIZEOF_TCP);
    try testing.expect(@sizeOf(std.posix.sockaddr.storage) <= XEV_SIZEOF_SOCKADDR);

    // Extended completion must be larger than base completion
    try testing.expect(@sizeOf(Completion) > @sizeOf(xev.Completion));
}

test "sockaddr ipv4" {
    var addr: xev_sockaddr = undefined;
    xev_sockaddr_ipv4(&addr, 127, 0, 0, 1, 8080);

    const port = xev_sockaddr_port(&addr);
    const testing = std.testing;
    try testing.expectEqual(@as(u16, 8080), port);
}

test "tcp init and bind" {
    const testing = std.testing;

    var tcp: xev_tcp = undefined;
    const init_result = xev_tcp_init(&tcp, XEV_AF_INET);
    try testing.expectEqual(@as(c_int, 0), init_result);

    // Bind to localhost on port 0 (auto-assign)
    var addr: xev_sockaddr = undefined;
    xev_sockaddr_ipv4(&addr, 127, 0, 0, 1, 0);

    const bind_result = xev_tcp_bind(&tcp, &addr);
    try testing.expectEqual(@as(c_int, 0), bind_result);

    // Get the assigned port
    var bound_addr: xev_sockaddr = undefined;
    const getsockname_result = xev_tcp_getsockname(&tcp, &bound_addr);
    try testing.expectEqual(@as(c_int, 0), getsockname_result);

    const bound_port = xev_sockaddr_port(&bound_addr);
    try testing.expect(bound_port > 0);

    // Clean up - close the socket directly since we're not using the event loop
    std.posix.close(xev_tcp_fd(&tcp));
}
