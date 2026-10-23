# 配置指南

首次使用时，请先按照[客户端专用密码获取指南](client-password.md)，在邮箱网页端通过 **头像 → 个人设置 → 安全设置 → 客户端专用密码** 生成专用密码。指南包含各步骤截图和配置填写说明。

默认读取 `~/.config/hust-mail-cli/config.json`（macOS 也使用此路径）：

```json
{
  "user": "你的邮箱@hust.edu.cn",
  "password": "你生成的专用密码",
  "protocol": "imap",
  "host": "mail.hust.edu.cn",
  "port": 993,
  "smtp": {
    "host": "mail.hust.edu.cn",
    "port": 465
  },
  "dir": "~/HUST-Mail/",
  "mailbox": "",
  "days": 7,
  "timeout": "30m"
}
```

服务器已预填为 IMAP `mail.hust.edu.cn:993` 和 SMTP `mail.hust.edu.cn:465`，均使用隐式 TLS（SSL）。只需填写 `user`（完整邮箱地址）和 `password`（网页端生成的专用密码）。`send` 使用 `smtp.host` / `smtp.port` 连接 SMTP，复用 `user` 和 `password`；加载配置本身不会连接或发信。

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

字段均可省略。收信默认主机为 `mail.hust.edu.cn`；`protocol` 仅接受 `imap`，`port: 0` 或省略时使用 IMAP TLS 端口 993。`mailbox` 留空表示所有可读取的 IMAP 文件夹。`timeout` 使用 `30m`、`60s` 等时长格式。默认归档目录为 `~/HUST-Mail/`，不受命令运行目录影响，适用于安装在系统目录中的命令。配置及 `--dir` 均支持开头的 `~/` 展开；相对路径仍相对于运行命令时的工作目录。已有配置中的 `dir` 会继续生效，需要使用新位置时请将其改为 `~/HUST-Mail/`。

`days` 控制下载的时间范围：`7` 表示本次同步启动前最近 7×24 小时，`0` 或省略表示不限时间，允许值为 0–36500。时间范围基于 IMAP 服务端的 `INTERNALDATE`（收信时间），而非邮件 `Date` 字段；按精确时间过滤，缺失收信时间时会报错。服务端日期搜索会适当放宽候选范围，再用收信时间核验，避免时区边界漏信。

时间范围只限制新增下载，不会删除已归档邮件，也不会自动滚动清理超过期限的本地文件。使用新配置字段前请先构建、安装对应版本 CLI。

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
| `--host` | `mail.hust.edu.cn` | 只填主机名，不含协议前缀或端口 |
| `--protocol` | `imap` | 仅接受 `imap` |
| `--port` | `0` | 使用 IMAP TLS 端口 993 |
| `--timeout` | `30m` | 连接、认证和同步的总时限 |
| `--config` | `~/.config/hust-mail-cli/config.json` | 指定配置文件的全局选项 |

不提供密码命令行参数。密码来自 `HUST_MAIL_PASSWORD`、配置或终端隐藏输入。`sync` 专用的 `--dir`、`--mailbox` 和 `--days` 见 [sync](command-reference.md#sync-同步邮件)。

## 连接与只读行为

默认使用 `mail.hust.edu.cn:993` 收信，使用 `mail.hust.edu.cn:465` 发信。可通过 `--host`、`--port` 覆盖收信地址。只支持隐式 TLS，验证服务器证书，最低 TLS 1.2。已有配置中的主机会继续生效；若旧配置的收信主机无法解析，需手动将 `host` 改为 `mail.hust.edu.cn`。更换主机会生成独立账号归档目录，保留旧归档，可能重复下载。

- IMAP 使用 `EXAMINE` 只读选择文件夹，使用 `BODY.PEEK[]` 读取，不标记已读，不修改标志，不执行删除、清除或上传命令。
- `send` 仅提供 SMTP 纯文本发送；暂不提供附件发送、回复、转发或 IMAP 写入。

`--timeout` 默认 30 分钟，覆盖连接、认证和同步，不包含终端输入密码的时间。连接阶段最多等待 30 秒；取消操作或总超时会关闭连接。

同步参数见 [sync](command-reference.md#sync-同步邮件)，归档目录和增量行为见[本地归档](archive.md)。

## SMTP 发信配置

`send` 使用独立的 `smtp.host`（默认 `mail.hust.edu.cn`）和 `smtp.port`（默认 `465`），与 IMAP 地址独立。使用隐式 TLS、系统信任库和最低 TLS 1.2，不支持明文、跳过证书校验或 STARTTLS。旧配置中显式设置的 SMTP 地址会保留；若旧地址无法解析，请手动将 `smtp.host` 改成 `mail.hust.edu.cn`。

账号和专用密码沿用配置的 `user`、`password`，支持 `HUST_MAIL_USER`、`HUST_MAIL_PASSWORD` 覆盖，`--user` 优先。`send` 不交互读取密码，未配置密码时报错。`--host` / `--port` 仅覆盖该次 SMTP 连接；`--timeout` 覆盖配置的总连接和提交超时。具体用法见[发信命令](command-reference.md#send-发送纯文本邮件)。
