# sync 同步邮件

只读连接邮箱，将缺失邮件保存为本地 `.eml`，包含正文和附件。默认使用 IMAP，同步所有可读取文件夹。

```sh
hust-mail-cli sync
hust-mail-cli sync --mailbox INBOX
hust-mail-cli sync --dir ~/HUST-Mail/
hust-mail-cli sync --days 7
hust-mail-cli sync --days 0
```

## 同步选项

| 参数 | 默认行为 | 说明 |
| --- | --- | --- |
| `--mailbox` | 所有可读取文件夹 | 指定一个 IMAP 文件夹；名称可先用 `folders` 查询 |
| `--dir` | `~/HUST-Mail/` | 本地归档目录，支持开头的 `~/` 展开 |
| `--days` | `0`，不限时间 | 新增下载的时间范围，可由配置覆盖；示例配置为 `7` |

连接选项 `--user`、`--host`、`--protocol`、`--port`、`--timeout` 和全局 `--config` 见[配置指南](../configuration.md)。显式参数覆盖配置。

## 时间范围

`--days 7` 表示本次同步启动前最近 7×24 小时，`--days 0` 明确覆盖配置并下载全部时间的邮件。允许值为 0–36500。

筛选依据是 IMAP 服务端 `INTERNALDATE`，不是发件人提供的 `Date`。服务器日期搜索适当放宽候选范围，再逐封核验精确收信时间；缺失收信时间时会报错。旧邮件不会下载正文，已归档文件不会因超过期限被自动删除。

## 增量与只读保证

使用 `EXAMINE` 和 `BODY.PEEK[]`，不标记已读，不修改、删除或上传远端邮件。重复运行只下载缺失或零字节文件；归档标识与 UIDVALIDITY 行为见[本地归档](../archive.md)。每个文件夹输出 `downloaded` 和 `existing` 计数，最后输出本次下载总数和归档位置。

中断后可直接重跑；SSH 后台运行和残留锁处理见[同步恢复](../troubleshooting.md)。

## POP3 兼容收信

```sh
hust-mail-cli sync --protocol pop3 --host pop.hust.edu.cn --port 995 --days 0
```

POP3 不支持文件夹选择和服务器收信时间筛选；配置中的 `mailbox` 必须为空。它与 IMAP 使用独立归档目录，不发送 `DELE`。下载范围和保留策略取决于服务端设置。


返回[命令参考](../command-reference.md)。
