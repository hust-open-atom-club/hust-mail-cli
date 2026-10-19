# 配置指南

首次使用时，请先按照[客户端专用密码获取指南](client-password.md)，在邮箱网页端通过 **头像 → 个人设置 → 安全设置 → 客户端专用密码** 生成专用密码。指南包含各步骤截图和配置填写说明。

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
hust-mail-cli folders
hust-mail-cli sync
hust-mail-cli list
```

字段均可省略。收信默认主机为 `imap.hust.edu.cn`；`port: 0` 或省略时按协议自动选择端口（IMAP 993、POP3 995）。兼容 POP3 时请设置 `host: "pop.hust.edu.cn"`、`port: 995` 和 `days: 0`；命令行切换时相应使用 `--protocol pop3 --host pop.hust.edu.cn --port 995 --days 0`。`mailbox` 留空表示所有可读取的 IMAP 文件夹，POP3 不支持此字段的非空值。`timeout` 使用 `30m`、`60s` 等时长格式。默认归档目录为 `~/HUST-Mail/`，不受命令运行目录影响，适用于安装在系统目录中的命令。配置及 `--dir` 均支持开头的 `~/` 展开；相对路径仍相对于运行命令时的工作目录。已有配置中的 `dir` 会继续生效，需要使用新位置时请将其改为 `~/HUST-Mail/`。

`days` 控制下载的时间范围：`7` 表示本次同步启动前最近 7×24 小时，`0` 或省略表示不限时间，允许值为 0–36500。时间范围基于 IMAP 服务端的 `INTERNALDATE`（收信时间），而非邮件 `Date` 字段；按精确时间过滤，缺失收信时间时会报错。服务端日期搜索会适当放宽候选范围，再用收信时间核验，避免时区边界漏信。

时间范围只限制新增下载，不会删除已归档邮件，也不会自动滚动清理超过期限的本地文件。POP3 不提供可靠的服务端收信时间，因此需要使用 `days: 0`，或切换为 IMAP。使用新配置字段前请先构建、安装对应版本 CLI。

优先级：显式命令行参数 > 环境变量 > 配置文件 > 默认值。环境变量支持 `HUST_MAIL_USER`、`HUST_MAIL_PASSWORD`。通过 `--config /path/to/config.json` 可指定其他文件；默认配置不存在时沿用参数或交互输入，显式指定的文件不存在则报错。无效配置或未知字段会报错；帮助、补全和 `read` 不读取配置文件。

也可省略配置中的密码，在启用相应收信服务后，运行命令并在终端隐藏输入专用密码：

```sh
hust-mail-cli folders --user 你的邮箱@hust.edu.cn
hust-mail-cli sync --user 你的邮箱@hust.edu.cn
```

CLI 不提供密码命令行参数，不输出密码，也不会把密码写入日志或邮件归档。专用密码的协议权限由邮箱服务端决定。

## 收信连接选项

以下选项适用于 `sync` 和 `folders`；显式参数覆盖环境变量及配置。

| 参数 | 内置默认值 | 说明 |
| --- | --- | --- |
| `--user` | 空 | 完整邮箱地址，也可通过 `HUST_MAIL_USER` 或配置提供 |
| `--host` | `imap.hust.edu.cn` | 只填主机名，不含协议前缀或端口 |
| `--protocol` | `imap` | `imap` 或兼容模式 `pop3` |
| `--port` | `0` | 自动按协议使用 IMAP 993 或 POP3 995 |
| `--timeout` | `30m` | 连接、认证和同步的总时限 |
| `--config` | `~/.config/hust-mail-cli/config.json` | 指定配置文件的全局选项 |

不提供密码命令行参数。密码来自 `HUST_MAIL_PASSWORD`、配置或终端隐藏输入。`sync` 专用的 `--dir`、`--mailbox` 和 `--days` 见 [sync](commands/sync.md)。

## 连接与只读行为

默认使用邮箱网页配置中提供的 `imap.hust.edu.cn:993` 收信，预留 `smtp.hust.edu.cn:465` 发信配置。POP3 兼容地址为 `pop.hust.edu.cn:995`。可通过 `--host`、`--port` 覆盖收信地址。只支持隐式 TLS，验证服务器证书，最低 TLS 1.2。已有配置中的主机会继续生效；更换主机会生成独立账号归档目录，保留旧归档，可能重复下载。

- IMAP 使用 `EXAMINE` 只读选择文件夹，使用 `BODY.PEEK[]` 读取，不标记已读，不修改标志，不执行删除、清除或上传命令。
- POP3 仅使用 `USER`、`PASS`、`UIDL`、`RETR` 和 `QUIT`，不发送 `DELE`。POP3 没有 IMAP 的文件夹和已读标志能力；下载范围取决于服务端 POP3 设置。服务端可能记录访问时间、下载状态或执行自身保留策略。
- 当前没有 SMTP、发送、回复、转发功能。后续可增加独立的 SMTP 模块。

`--timeout` 默认 30 分钟，覆盖连接、认证和同步，不包含终端输入密码的时间。连接阶段最多等待 30 秒；取消操作或总超时会关闭连接。

同步参数见 [sync](commands/sync.md)，归档目录和增量行为见[本地归档](archive.md)。
