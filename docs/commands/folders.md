# folders 列出文件夹

只读连接 IMAP 邮箱，列出可选择的文件夹，便于确定 `sync --mailbox` 的名称。

```sh
hust-mail-cli folders
hust-mail-cli folders --user 你的邮箱@hust.edu.cn
hust-mail-cli folders --config /path/to/config.json
```

输出每行一个带引号的文件夹名称，例如 `"INBOX"`。将实际名称传给同步命令：

```sh
hust-mail-cli sync --mailbox INBOX
```

支持 `--user`、`--host`、`--protocol`、`--port` 和 `--timeout`；连接与凭据规则见[配置指南](../configuration.md)。此命令仅适用于 IMAP，不下载正文，不修改远端邮件，也不创建本地归档。


返回[命令参考](../command-reference.md)。
