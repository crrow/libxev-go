// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

const xev = @import("xev");

pub const tcp = @import("tcp_api.zig");
pub const file = @import("file_api.zig");
pub const udp = @import("udp_api.zig");

export fn xev_loop_set_thread_pool(loop: *xev.Loop, pool: *xev.ThreadPool) void {
    loop.thread_pool = pool;
}

comptime {
    _ = tcp;
    _ = file;
    _ = udp;
}

test {
    _ = tcp;
    _ = file;
    _ = udp;
}
