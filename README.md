# hust-mail-cli

Go 编写的华中科技大学邮箱 CLI，使用 Cobra 组织命令。当前支持 IMAP/POP3 收信、本地增量归档和本地读取，SMTP 留待后续实现。

## 构建

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

下文示例中的 `./hust-mail-cli` 适用于直接用 `go build -o hust-mail-cli ./cmd/hust-mail-cli` 构建的文件；使用 Makefile 时替换为 `./bin/hust-mail-cli`，安装后使用 `hust-mail-cli`。

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

## 配置与专用密码

默认读取 `~/.config/hust-mail-cli/config.json`（macOS 也使用此路径）：

```json
{
  "user": "你的邮箱@hust.edu.cn",
  "password": "你生成的专用密码",
  "protocol": "imap",
  "host": "mail.hust.edu.cn",
  "port": 0,
  "dir": "~/HUST-Mail/",
  "mailbox": "",
  "timeout": "30m"
}
```

可从仓库中的 `config.example.json` 复制后填写：

```sh
mkdir -p ~/.config/hust-mail-cli
cp -n config.example.json ~/.config/hust-mail-cli/config.json
chmod 700 ~/.config/hust-mail-cli
chmod 600 ~/.config/hust-mail-cli/config.json
```

配置中的 `password` 填邮箱专用密码；文件以明文保存，请保持 `0600` 权限。加载器会拒绝含非空密码且组或其他用户有权限的配置文件，并提示执行 `chmod 600`；仅本人可读的 `0400` 也可使用。填写后直接运行：

```sh
./hust-mail-cli folders
./hust-mail-cli sync
./hust-mail-cli list
```

字段均可省略。`port: 0` 按协议自动选择端口（IMAP 993、POP3 995），切换协议时无需保留另一协议的端口。`mailbox` 留空表示所有可读取的 IMAP 文件夹，POP3 不支持此字段的非空值。`timeout` 使用 `30m`、`60s` 等时长格式。默认归档目录为 `~/HUST-Mail/`，不受命令运行目录影响，适用于安装在系统目录中的命令。配置及 `--dir` 均支持开头的 `~/` 展开；相对路径仍相对于运行命令时的工作目录。已有配置中的 `dir` 会继续生效，需要使用新位置时请将其改为 `~/HUST-Mail/`。

优先级：显式命令行参数 > 环境变量 > 配置文件 > 默认值。环境变量支持 `HUST_MAIL_USER`、`HUST_MAIL_PASSWORD`。通过 `--config /path/to/config.json` 可指定其他文件；默认配置不存在时沿用参数或交互输入，显式指定的文件不存在则报错。无效配置或未知字段会报错；帮助、补全和 `read` 不读取配置文件。

也可省略配置中的密码，在启用相应收信服务后，运行命令并在终端隐藏输入专用密码：

```sh
./hust-mail-cli folders --user 你的邮箱@hust.edu.cn
./hust-mail-cli sync --user 你的邮箱@hust.edu.cn
```

CLI 不提供密码命令行参数，不输出密码，也不会把密码写入日志或邮件归档。专用密码的协议权限由邮箱服务端决定。

## 命令

```sh
# IMAP：默认同步所有可读取文件夹
./hust-mail-cli sync --user 你的邮箱@hust.edu.cn --dir ~/HUST-Mail/

# IMAP：只同步指定文件夹
./hust-mail-cli sync --user 你的邮箱@hust.edu.cn --mailbox INBOX

# POP3：下载服务端 POP3 邮件列表提供的邮件
./hust-mail-cli sync --protocol pop3 --user 你的邮箱@hust.edu.cn

# 本地列表：每行一个 JSON，包含 path、subject、from、date
./hust-mail-cli list --dir ~/HUST-Mail/

# 读取本地完整 MIME 原文
./hust-mail-cli read ~/HUST-Mail/账号目录/文件夹目录/UIDVALIDITY/7.eml

# 生成 shell 补全脚本
./hust-mail-cli completion zsh
```

`read` 输出原始 `.eml`，尚未提供 MIME 正文渲染或单独导出附件。`list` 会解码邮件主题；发件人保留原始头字段值。本地命令不连接服务器。

## 连接与只读行为

默认主机为 `mail.hust.edu.cn`。华科[官方说明](https://ncc.hust.edu.cn/info/1445/6060.htm)要求启用 SSL，并明确 IMAP 接收端口为 993。POP3 默认使用标准 TLS 端口 995；此端口尚未用真实华科账号验证。可通过 `--host`、`--port` 覆盖。只支持隐式 TLS，验证服务器证书，最低 TLS 1.2。

- IMAP 使用 `EXAMINE` 只读选择文件夹，使用 `BODY.PEEK[]` 读取，不标记已读，不修改标志，不执行删除、清除或上传命令。
- POP3 仅使用 `USER`、`PASS`、`UIDL`、`RETR` 和 `QUIT`，不发送 `DELE`。POP3 没有 IMAP 的文件夹和已读标志能力；下载范围取决于服务端 POP3 设置。服务端可能记录访问时间、下载状态或执行自身保留策略。
- 当前没有 SMTP、发送、回复、转发功能。后续可增加独立的 SMTP 模块。

`--timeout` 默认 30 分钟，覆盖连接、认证和同步，不包含终端输入密码的时间。连接阶段最多等待 30 秒；取消操作或总超时会关闭连接。

## 本地归档

完整邮件以 `.eml` 保存，包含正文和附件。账号目录按协议、服务器、端口、账号隔离；IMAP 再按 base64url 文件夹名和 UIDVALIDITY 分目录。POP3 以 UIDL 的 SHA-256 作为文件名，要求服务器支持稳定的 UIDL。

重复同步只下载缺失或零字节文件；服务器删除邮件不会删除本地归档。IMAP 的 UIDVALIDITY 改变时保留旧目录并新建目录，可能产生内容重复。IMAP 与 POP3 使用独立目录，同一封邮件通过两种协议同步会各存一份。

目录权限为 `0700`，邮件文件为 `0600`。下载先写临时文件，成功后才发布最终文件；失败可重跑。工具不会校验已有非空文件的完整性，也不提供归档加密。

同一账号使用 `.sync.lock` 防止并发同步。强制终止可能遗留锁和 `.download-*` 临时文件；确认该账号没有同步进程运行后，可手动删除它们，再运行同步。

## 项目结构与验证

参考 `atomgit-cli` 的命令构造方式：`cmd/hust-mail-cli` 为入口，`pkg/cmd/root` 使用 `NewCmdRoot`、`RunE`、`ExecuteContext`，`internal/mailarchive` 处理收信和本地存储。

```sh
go test -race ./...
go vet ./...
```

协议测试使用内存连接，限制实际发送的命令，并覆盖只读读取、增量同步、UIDVALIDITY 变更、POP3 点转义和下载中断。尚未连接真实华科邮箱进行验证。
