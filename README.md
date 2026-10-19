# hust-mail-cli

Go 编写的华中科技大学邮箱 CLI，使用 Cobra 组织命令。默认通过只读 IMAP 收信，支持本地增量归档、搜索、正文展示和附件导出。后续发信使用 SMTP，目前尚未实现；POP3 保留为显式选择的兼容收信方式。

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

首次使用时，请先按照[客户端专用密码获取指南](docs/client-password.md)，在邮箱网页端通过 **头像 → 个人设置 → 安全设置 → 客户端专用密码** 生成专用密码。指南包含各步骤截图和配置填写说明。

默认读取 `~/.config/hust-mail-cli/config.json`（macOS 也使用此路径）：

```json
{
  "user": "你的邮箱@hust.edu.cn",
  "password": "你生成的专用密码",
  "protocol": "imap",
  "host": "imap.hust.edu.cn",
  "port": 993,
  "smtp": {
    "host": "smtp.hust.edu.cn",
    "port": 465
  },
  "dir": "~/HUST-Mail/",
  "mailbox": "",
  "days": 7,
  "timeout": "30m"
}
```

服务器已预填为 IMAP `imap.hust.edu.cn:993` 和 SMTP `smtp.hust.edu.cn:465`，均使用隐式 TLS（SSL）。只需填写 `user`（完整邮箱地址）和 `password`（网页端生成的专用密码）。`smtp` 当前仅预留配置，不建立连接或启用发信。

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

字段均可省略。收信默认主机为 `imap.hust.edu.cn`；`port: 0` 或省略时按协议自动选择端口（IMAP 993、POP3 995）。兼容 POP3 时请设置 `host: "pop.hust.edu.cn"`、`port: 995` 和 `days: 0`；命令行切换时相应使用 `--protocol pop3 --host pop.hust.edu.cn --port 995 --days 0`。`mailbox` 留空表示所有可读取的 IMAP 文件夹，POP3 不支持此字段的非空值。`timeout` 使用 `30m`、`60s` 等时长格式。默认归档目录为 `~/HUST-Mail/`，不受命令运行目录影响，适用于安装在系统目录中的命令。配置及 `--dir` 均支持开头的 `~/` 展开；相对路径仍相对于运行命令时的工作目录。已有配置中的 `dir` 会继续生效，需要使用新位置时请将其改为 `~/HUST-Mail/`。

`days` 控制下载的时间范围：`7` 表示本次同步启动前最近 7×24 小时，`0` 或省略表示不限时间，允许值为 0–36500。时间范围基于 IMAP 服务端的 `INTERNALDATE`（收信时间），而非邮件 `Date` 字段；按精确时间过滤，缺失收信时间时会报错。服务端日期搜索会适当放宽候选范围，再用收信时间核验，避免时区边界漏信。

时间范围只限制新增下载，不会删除已归档邮件，也不会自动滚动清理超过期限的本地文件。POP3 不提供可靠的服务端收信时间，因此需要使用 `days: 0`，或切换为 IMAP。使用新配置字段前请先构建、安装对应版本 CLI。

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

# IMAP：只下载近 7 天收到的邮件；覆盖配置中的范围
./hust-mail-cli sync --days 7

# 显式覆盖 days 配置，下载全部时间的邮件
./hust-mail-cli sync --days 0

# POP3：下载服务端 POP3 邮件列表提供的邮件
./hust-mail-cli sync --protocol pop3 --host pop.hust.edu.cn --port 995 --days 0 --user 你的邮箱@hust.edu.cn

# 本地列表：每行一个 JSON，包含 path、subject、from、date
./hust-mail-cli list --dir ~/HUST-Mail/

# 展示本地解码后的邮件正文
./hust-mail-cli read ~/HUST-Mail/账号目录/文件夹目录/UIDVALIDITY/7.eml

# 输出完整 MIME 原文（保留旧版 read 的原文输出方式）
./hust-mail-cli read --raw /path/to/message.eml

# 搜索本地解码后的主题、发件人、收件人和正文
./hust-mail-cli search "会议"
./hust-mail-cli search --from "example.com" --subject "通知" --limit 20
./hust-mail-cli search --body "项目进展" --dir ~/HUST-Mail/

# 列出附件编号、文件名、类型及解码后大小（JSON Lines）
./hust-mail-cli attachments list /path/to/message.eml

# 导出全部附件，或仅导出第 2 个附件
./hust-mail-cli attachments export /path/to/message.eml --output ~/Downloads/mail-attachments
./hust-mail-cli attachments export /path/to/message.eml --index 2 --output ./attachments

# 生成 shell 补全脚本
./hust-mail-cli completion zsh
```

`read` 默认解码 MIME 正文和字符集，优先展示纯文本；仅有 HTML 时转换为文本，省略脚本、样式，不执行代码或加载外部资源。展示时过滤终端控制字符；`--raw` 保留原始字节。每封邮件最多解析 8 MiB 解码后文本，超限或无法解码时明确报错，可改用 `--raw`。

`search` 按不区分大小写的字面子串匹配，位置参数与各字段条件组合时取 AND。正文使用与 `read` 相同的文本选择规则，不搜索附件内容；输出 JSON Lines，含绝对路径、主题、发件人、收件人、日期及附件数量。默认不限制结果数量；无匹配时输出为空并成功退出。搜索逐封扫描本地 `.eml`，不建立索引；遇到无法解析的邮件会报错。

附件编号从 1 开始，包含带文件名的内联资源（如图片）。附件只解码 Base64、Quoted-Printable 等传输编码，保留原始字符集字节，不将 GBK 等文本附件转换为 UTF-8；列表中的大小对应实际导出字节数。导出的文件带 `001-` 等编号前缀，移除原始文件名中的目录路径，权限为 `0600`。导出不覆盖已有文件或符号链接；解码失败会清理临时文件。`--index 0` 或省略编号表示导出全部附件。

`list` 会解码邮件主题；发件人保留原始头字段值。上述本地命令均不连接服务器，不改变原始归档，也不修改邮件的远端状态。附件导出只向指定输出目录写文件。

## 连接与只读行为

默认使用邮箱网页配置中提供的 `imap.hust.edu.cn:993` 收信，预留 `smtp.hust.edu.cn:465` 发信配置。POP3 兼容地址为 `pop.hust.edu.cn:995`。可通过 `--host`、`--port` 覆盖收信地址。只支持隐式 TLS，验证服务器证书，最低 TLS 1.2。已有配置中的主机会继续生效；更换主机会生成独立账号归档目录，保留旧归档，可能重复下载。

- IMAP 使用 `EXAMINE` 只读选择文件夹，使用 `BODY.PEEK[]` 读取，不标记已读，不修改标志，不执行删除、清除或上传命令。
- POP3 仅使用 `USER`、`PASS`、`UIDL`、`RETR` 和 `QUIT`，不发送 `DELE`。POP3 没有 IMAP 的文件夹和已读标志能力；下载范围取决于服务端 POP3 设置。服务端可能记录访问时间、下载状态或执行自身保留策略。
- 当前没有 SMTP、发送、回复、转发功能。后续可增加独立的 SMTP 模块。

`--timeout` 默认 30 分钟，覆盖连接、认证和同步，不包含终端输入密码的时间。连接阶段最多等待 30 秒；取消操作或总超时会关闭连接。

## 本地归档

完整邮件以 `.eml` 保存，包含正文和附件。账号目录按协议、服务器、端口、账号隔离；IMAP 再按 base64url 文件夹名和 UIDVALIDITY 分目录。POP3 以 UIDL 的 SHA-256 作为文件名，要求服务器支持稳定的 UIDL。

重复同步只下载缺失或零字节文件；服务器删除邮件不会删除本地归档。IMAP 的 UIDVALIDITY 改变时保留旧目录并新建目录，可能产生内容重复。IMAP 与 POP3 使用独立目录，同一封邮件通过两种协议同步会各存一份。

目录权限为 `0700`，邮件文件为 `0600`。下载先写临时文件，成功后才发布最终文件；失败可重跑。工具不会校验已有非空文件的完整性，也不提供归档加密。

## 异常退出与同步恢复

中断后直接重跑 `hust-mail-cli sync`，已完成的邮件会自动跳过，无需额外修复命令。

```sh
# SSH 后台长期同步
nohup hust-mail-cli sync > ~/hust-mail-sync.log 2>&1 < /dev/null &
```

同步使用系统 `flock` 独占锁，即使进程被 `kill -9` 结束，内核也会释放它。新版残留的 PID 标记会在下一次同步时自动回收。旧版纯 PID 锁只有在确认原进程不存在时才会回收；PID 仍存活、无权检查或标记无法识别时，工具拒绝自动删除，防止误抢正在使用的归档。

`.sync.flock` 是长期保留的内核锁文件，存在本身是正常现象，**不要手工删除它**。异常退出留下的 `.download-*`、`.lock-*` 临时文件不会阻止重跑；如需清理，应在确认该账号没有同步进程运行后操作，保留 `.eml` 邮件。

程序处理 `SIGHUP`、`SIGINT` 和 `SIGTERM`，收到信号后取消操作并清理锁。SSH 断开可能终止裸 `&` 后台任务；这不代表任务会持续运行，但重跑不会因新版残留锁而被永久阻塞。希望断开连接后继续同步，请使用 `nohup` 或 `tmux`。锁机制面向 Linux/macOS 本地文件系统；不支持锁的文件系统会明确报错。

## 更多文档

- [客户端专用密码获取指南](docs/client-password.md)：网页端操作路径、截图及 CLI 配置填写。
- [配置示例](config.example.json)：预填华科邮箱服务器，只需填写邮箱和专用密码。

## 项目结构与验证

参考 `atomgit-cli` 的命令构造方式：`cmd/hust-mail-cli` 为入口，`pkg/cmd/root` 使用 `NewCmdRoot`、`RunE`、`ExecuteContext`，`internal/mailarchive` 处理收信和本地存储。

```sh
go test -race ./...
go vet ./...
```

协议测试使用内存连接，限制实际发送的命令，并覆盖只读读取、增量同步、UIDVALIDITY 变更、POP3 点转义和下载中断。MIME 测试覆盖字符集与传输编码、正文选择、HTML 转文本、搜索过滤，以及附件导出的路径与覆盖保护。

真实账号已验证 IMAP 的小文件夹同步、重跑及标志不变；POP3 已验证登录与 UIDL，尚未验证 CLI 下载。新增本地功能已用 macOS 上 138 封真实归档验证，并在 Linux 主机上执行原生测试和归档读取检查；未进行全量邮箱同步。
