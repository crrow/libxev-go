// MIT License
// Copyright (c) 2023 Mitchell Hashimoto
// Copyright (c) 2026 Crrow

const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const libxev_dep = b.dependency("libxev", .{
        .target = target,
        .optimize = optimize,
    });

    const root_module = b.createModule(.{
        .root_source_file = b.path("root.zig"),
        .target = target,
        .optimize = optimize,
    });
    root_module.addImport("xev", libxev_dep.module("xev"));

    const dynamic_lib = b.addLibrary(.{
        .linkage = .dynamic,
        .name = "xev_extended",
        .root_module = root_module,
    });
    dynamic_lib.linkLibC();
    b.installArtifact(dynamic_lib);

    const static_lib_module = b.createModule(.{
        .root_source_file = b.path("root.zig"),
        .target = target,
        .optimize = optimize,
    });
    static_lib_module.addImport("xev", libxev_dep.module("xev"));

    const static_lib = b.addLibrary(.{
        .linkage = .static,
        .name = "xev_extended_static",
        .root_module = static_lib_module,
    });
    static_lib.linkLibC();
    b.installArtifact(static_lib);

    const test_module = b.createModule(.{
        .root_source_file = b.path("root.zig"),
        .target = target,
        .optimize = optimize,
    });
    test_module.addImport("xev", libxev_dep.module("xev"));

    const tests = b.addTest(.{
        .name = "extended_api_test",
        .root_module = test_module,
    });
    tests.linkLibC();

    const run_tests = b.addRunArtifact(tests);
    const test_step = b.step("test", "Run unit tests");
    test_step.dependOn(&run_tests.step);
}
