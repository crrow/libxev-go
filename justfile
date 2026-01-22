# Load .env file automatically
set dotenv-load := true

# ========================================================================================
# Environment Variables
# ========================================================================================

GO := env("GO", "go")
ZIG := env("ZIG", "zig")
GOLANGCI_LINT := env("GOLANGCI_LINT", "golangci-lint")
HAWKEYE := env("HAWKEYE", "hawkeye")
GOSEC := env("GOSEC", "gosec")
GOVULNCHECK := env("GOVULNCHECK", "govulncheck")
GOIMPORTS := env("GOIMPORTS", "goimports")

BIN_DIR := env("BIN_DIR", "bin")
MODULE_NAME := env("MODULE_NAME", "github.com/crrow/libxev-go")
DOCS_PORT := env("DOCS_PORT", "7070")

# Platform-specific library name
LIBXEV_NAME := if os() == "macos" { "libxev.dylib" } else if os() == "windows" { "xev.dll" } else { "libxev.so" }
LIBXEV_PATH := justfile_directory() / "deps/libxev/zig-out/lib" / LIBXEV_NAME

# Extended library (TCP API) - platform-specific name
LIBXEV_EXT_NAME := if os() == "macos" { "libxev_extended.dylib" } else if os() == "windows" { "xev_extended.dll" } else { "libxev_extended.so" }
LIBXEV_EXT_PATH := justfile_directory() / "zig/zig-out/lib" / LIBXEV_EXT_NAME

# Version information (for build-time injection)
# Try to get version from git tag first, fallback to version.go
GIT_TAG := trim(`git describe --tags --exact-match 2>/dev/null || echo ""`)
VERSION_FROM_FILE := trim(`grep -o 'Version = ".*"' version.go | cut -d'"' -f2 2>/dev/null || echo "dev"`)
VERSION := if GIT_TAG != "" { trim_start_match(GIT_TAG, "v") } else { VERSION_FROM_FILE }
GIT_COMMIT := trim(`git rev-parse --short HEAD 2>/dev/null || echo "unknown"`)
BUILD_TIME := trim(`date -u '+%Y-%m-%d_%H:%M:%S'`)

# Build flags for version injection (using lowercase variable names to match cmd/version.go)
LDFLAGS := "-s -w -X main.version=" + VERSION + " -X main.gitCommit=" + GIT_COMMIT + " -X main.buildTime=" + BUILD_TIME

# ========================================================================================
# Help
# ========================================================================================

[group("Help")]
[private]
default:
    @just --list --list-heading 'libxev-go justfile manual page:\n'

[doc("show help")]
[group("Help")]
help: default

[doc("show version information")]
[group("Help")]
version:
    @echo "Version:    {{ VERSION }}"
    @echo "Git Tag:    {{ GIT_TAG }}"
    @echo "Git Commit: {{ GIT_COMMIT }}"
    @echo "Build Time: {{ BUILD_TIME }}"

# ========================================================================================
# Build
# ========================================================================================

[doc("build libxev native library")]
[group("Build")]
build-libxev:
    @echo "Building libxev..."
    cd deps/libxev && {{ ZIG }} build -Doptimize=ReleaseFast
    @echo "Done: {{ LIBXEV_PATH }}"

[doc("build extended C API library (TCP support)")]
[group("Build")]
build-extended:
    @echo "Building extended C API (TCP)..."
    cd zig && {{ ZIG }} build -Doptimize=ReleaseFast
    @echo "Done: {{ LIBXEV_EXT_PATH }}"

[doc("build all native libraries")]
[group("Build")]
build-all-native: build-libxev build-extended
    @echo "Done: All native libraries built!"

[doc("clean libxev build artifacts")]
[group("Build")]
clean-libxev:
    @echo "Cleaning libxev build artifacts..."
    rm -rf deps/libxev/zig-out deps/libxev/.zig-cache
    @echo "Done!"

[doc("clean extended library build artifacts")]
[group("Build")]
clean-extended:
    @echo "Cleaning extended library build artifacts..."
    rm -rf zig/zig-out zig/.zig-cache
    @echo "Done!"

[doc("build Go packages")]
[group("Build")]
build: build-all-native
    @echo "Building Go packages..."
    {{ GO }} build ./...
    @echo "Done!"

[doc("clean all build artifacts")]
[group("Build")]
clean: clean-libxev clean-extended
    @echo "Cleaning Go build artifacts..."
    {{ GO }} clean ./...
    rm -rf {{ BIN_DIR }}
    @echo "Done!"

[doc("update version.go based on latest git tag")]
[group("Help")]
version-update:
    #!/usr/bin/env bash
    set -euo pipefail
    LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    if [ -z "$LATEST_TAG" ]; then
        echo "Error: No git tags found. Create a tag first with: git tag v0.1.0"
        exit 1
    fi
    VERSION="${LATEST_TAG#v}"
    echo "Updating version.go to: $VERSION (from tag: $LATEST_TAG)"
    sed -i.bak "s/const Version = \".*\"/const Version = \"$VERSION\"/" version.go
    rm version.go.bak
    echo "Done: version.go updated to version $VERSION"
    echo "Tip: Don't forget to commit this change!"

[doc("create a new git tag and update version.go")]
[group("Help")]
version-tag VERSION_NUM:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! "{{ VERSION_NUM }}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
        echo "Error: Invalid version format. Use: x.y.z or x.y.z-suffix"
        echo "   Examples: 0.1.0, 1.0.0-beta, 2.1.3-rc1"
        exit 1
    fi
    TAG="v{{ VERSION_NUM }}"
    echo "Creating git tag: $TAG"
    git tag -a "$TAG" -m "Release $TAG"
    echo "Updating version.go..."
    sed -i.bak "s/const Version = \".*\"/const Version = \"{{ VERSION_NUM }}\"/" version.go
    rm version.go.bak
    echo "Done: Tag created and version.go updated!"
    echo ""
    echo "Next steps:"
    echo "  1. Review changes: git diff version.go"
    echo "  2. Commit: git add version.go && git commit -m 'chore: bump version to {{ VERSION_NUM }}'"
    echo "  3. Push tag: git push origin $TAG"

[doc("run `go fmt` and `goimports` to format code")]
[group("Code Quality")]
fmt: hawkeye-fix
    @echo "Formatting code..."
    find . -name "*.go" ! -path "./.history/*" ! -path "./vendor/*" -exec gofmt -w -s {} +
    find . -name "*.go" ! -path "./.history/*" ! -path "./vendor/*" -exec {{ GOIMPORTS }} -w -local {{ MODULE_NAME }} {} +
    @echo "Done: Code formatted!"

[doc("check code formatting")]
[group("Code Quality")]
fmt-check:
    @echo "Checking code formatting..."
    @test -z "$(find . -name '*.go' ! -path './.history/*' ! -path './vendor/*' -exec gofmt -l {} +)" || (echo "Error: Code is not formatted. Run 'just fmt'" && exit 1)
    @echo "Done: Code formatting is correct!"

[doc("run `golangci-lint`")]
[group("Code Quality")]
lint:
    @echo "Running linter..."
    {{ GOLANGCI_LINT }} run --timeout 5m
    @echo "Done: Linter checks passed!"

alias l := lint

[doc("run `golangci-lint` with auto-fix")]
[group("Code Quality")]
lint-fix:
    @echo "Running linter with auto-fix..."
    {{ GOLANGCI_LINT }} run --fix --timeout 5m
    @echo "Done: Auto-fix completed!"

[doc("run `fmt` and `lint-fix` at once")]
[group("Code Quality")]
fix: fmt lint-fix
    @echo "Done: All fixes applied!"

[doc("run `fmt-check`, `lint`, and `test` at once")]
[group("Code Quality")]
check: fmt-check lint
    {{ GO }} vet ./...
    @echo "Done: All quality checks passed!"

alias c := check

[doc("run unit tests only")]
[group("Testing")]
test: build-all-native
    @echo "Running unit tests..."
    LIBXEV_PATH={{ LIBXEV_PATH }} LIBXEV_EXT_PATH={{ LIBXEV_EXT_PATH }} {{ GO }} test -count=1 -v -race -cover ./...
    @echo "Done: Unit tests passed!"

[doc("run unit tests (skip native build if already built)")]
[group("Testing")]
test-quick:
    @echo "Running unit tests (quick)..."
    @test -f {{ LIBXEV_PATH }} || just build-libxev
    @test -f {{ LIBXEV_EXT_PATH }} || just build-extended
    LIBXEV_PATH={{ LIBXEV_PATH }} LIBXEV_EXT_PATH={{ LIBXEV_EXT_PATH }} {{ GO }} test -count=1 -v -race -cover ./...
    @echo "Done: Unit tests passed!"

[doc("run extended library Zig tests")]
[group("Testing")]
test-zig:
    @echo "Running extended library Zig tests..."
    cd zig && {{ ZIG }} build test
    @echo "Done: Zig tests passed!"

# ========================================================================================
# License Management
# ========================================================================================

[doc("check license headers")]
[group("License")]
hawkeye: hawkeye-check

[group("License")]
[private]
hawkeye-check:
    @echo "Checking license headers with hawkeye..."
    @command -v {{ HAWKEYE }} >/dev/null 2>&1 || (echo "Error: hawkeye not found. Run 'just init' to install" && exit 1)
    {{ HAWKEYE }} check
    @echo "Done: License headers are correct!"

[doc("fix license headers")]
[group("License")]
hawkeye-fix:
    @echo "Fixing license headers with hawkeye..."
    @command -v {{ HAWKEYE }} >/dev/null 2>&1 || (echo "Error: hawkeye not found. Run 'just init' to install" && exit 1)
    {{ HAWKEYE }} format
    @echo "Done: License headers fixed!"


# ========================================================================================
# Documentation
# ========================================================================================

[doc("serve documentation with mdbook")]
[group("📚 Documentation")]
book:
    @echo "📚 Serving documentation..."
    mdbook serve docs --port 13000

[doc("build documentation with mdbook")]
[group("📚 Documentation")]
docs-build:
    @echo "📚 Building documentation..."
    mdbook build docs

[doc("open cargo docs in browser")]
[group("📚 Documentation")]
docs-open:
    @echo "📚 Opening cargo documentation..."
    cargo doc --workspace --all-features --no-deps --document-private-items --open