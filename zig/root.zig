// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

const xev = @import("xev");

pub const tcp = @import("tcp_api.zig");
pub const file = @import("file_api.zig");
pub const udp = @import("udp_api.zig");

// Initialize a loop with options including thread pool support.
// This replaces the old xev_loop_set_thread_pool pattern which is no longer
// supported by libxev. Thread pools must now be passed during initialization.
export fn xev_loop_init_with_options(loop: *xev.Loop, options: *const xev.Options) c_int {
    const result = xev.Loop.init(options.*) catch {
        return -1;
    };
    loop.* = result;
    return 0;
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
