#!/bin/sh
# Cross-compile without a C toolchain. Run from any working directory.
set -eu
cd "$(dirname "$0")/.."
GO=${GO:-go}
BIN_DIR=${BIN_DIR:-bin}

if [ "$#" -eq 0 ]; then
    set -- \
        linux/386 linux/amd64 linux/arm linux/arm64 linux/loong64 \
        linux/mips linux/mipsle linux/mips64 linux/mips64le \
        linux/ppc64 linux/ppc64le linux/riscv64 linux/s390x \
        darwin/amd64 darwin/arm64
fi

for target do
    case "$target" in
        linux/386|linux/amd64|linux/arm|linux/arm64|linux/loong64|\
        linux/mips|linux/mipsle|linux/mips64|linux/mips64le|\
        linux/ppc64|linux/ppc64le|linux/riscv64|linux/s390x|\
        darwin/amd64|darwin/arm64) ;;
        *) echo "Unsupported target: $target" >&2; exit 1 ;;
    esac
done

for target do
    target_os=${target%/*}
    target_arch=${target#*/}
    echo "Building $target"
    mkdir -p "$BIN_DIR/$target_os-$target_arch"
    # Explicit CPU baselines make output independent of host Go tuning flags.
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
        GOAMD64=v1 GO386=sse2 GOARM=7 GOARM64=v8.0 \
        GOMIPS=softfloat GOMIPS64=softfloat GOPPC64=power8 \
        GORISCV64=rva20u64 \
        "$GO" build -trimpath -o "$BIN_DIR/$target_os-$target_arch/hust-mail-cli" ./cmd/hust-mail-cli
done
