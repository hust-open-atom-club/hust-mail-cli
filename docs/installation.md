# 安装与构建

以下命令在仓库根目录执行。

需要 Go 1.26 或更新版本。

```sh
make
./bin/hust-mail-cli --help
make install
```

`make` 默认构建 `bin/hust-mail-cli`；`make install` 安装到 `GOBIN`，未设置时使用 `GOPATH` 第一项的 `bin` 目录，将该目录加入 `PATH` 后可直接运行 `hust-mail-cli`。例如：

```sh
make install GOBIN="$HOME/.local/bin"
hust-mail-cli --help
```

常用目标：`make test`、`make test-race`、`make fmt`、`make lint`、`make check`、`make coverage`、`make cross-build`、`make clean`。`make help` 列出全部目标；`make uninstall` 删除同一安装位置的可执行文件。`clean` 和 `uninstall` 不删除配置或邮件。

本文档的命令示例默认使用已安装的 `hust-mail-cli`；尚未安装时，在仓库根目录替换为 `./bin/hust-mail-cli`。

## 支持的平台

面向 Linux 和 macOS，不支持 Windows。使用 `CGO_ENABLED=0` 构建，无需 C 编译器，也不依赖系统邮件客户端。

| 系统 | 交叉构建目标（Go 架构名） |
| --- | --- |
| Linux | `amd64`、`arm64`、`386`、`arm`、`loong64`、`riscv64`、`ppc64`、`ppc64le`、`s390x`、`mips`、`mipsle`、`mips64`、`mips64le` |
| macOS | `amd64`（Intel）、`arm64`（Apple Silicon） |

```sh
# 构建全部目标，产物位于 bin/<系统>-<架构>/hust-mail-cli
sh scripts/build-platforms.sh

# 只构建指定目标
sh scripts/build-platforms.sh linux/loong64 linux/riscv64 darwin/arm64

# 等价的 Makefile 入口
make cross-build TARGETS="linux/loong64 linux/riscv64 darwin/arm64"

# 本机安装到 GOBIN，未设置时安装到 GOPATH/bin
go install ./cmd/hust-mail-cli
```

构建脚本使用保守 CPU 基线：amd64 v1、ARMv7、ARM64 v8.0、POWER8、RISC-V rva20u64；MIPS 使用 softfloat，386 使用 SSE2。更旧 CPU 需要另行评估或调整构建参数。系统最低版本由使用的 Go 工具链决定。

Linux/macOS 均使用 `~/.config/hust-mail-cli/config.json` 和默认归档目录 `~/HUST-Mail/`，无需在安装目录写配置或邮件。CI 配置覆盖 Linux/macOS 原生测试及上述目标的交叉构建；交叉编译成功不等于已经在对应 CPU 上运行验证。

## 卸载

```sh
make uninstall
# 自定义安装目录时使用相同的 GOBIN
make uninstall GOBIN="$HOME/.local/bin"
```

卸载只移除可执行文件，保留配置及邮件归档。
