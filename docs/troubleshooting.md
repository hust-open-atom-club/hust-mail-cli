# 同步恢复与故障排查

## 中断与残留锁

中断后直接重跑 `hust-mail-cli sync`，已完成的邮件会自动跳过，无需额外修复命令。

```sh
# SSH 后台长期同步
nohup hust-mail-cli sync > ~/hust-mail-sync.log 2>&1 < /dev/null &
```

同步使用系统 `flock` 独占锁，即使进程被 `kill -9` 结束，内核也会释放它。新版残留的 PID 标记会在下一次同步时自动回收。旧版纯 PID 锁只有在确认原进程不存在时才会回收；PID 仍存活、无权检查或标记无法识别时，工具拒绝自动删除，防止误抢正在使用的归档。

`.sync.flock` 是长期保留的内核锁文件，存在本身是正常现象，**不要手工删除它**。异常退出留下的 `.download-*`、`.lock-*` 临时文件不会阻止重跑；如需清理，应在确认该账号没有同步进程运行后操作，保留 `.eml` 邮件。

程序处理 `SIGHUP`、`SIGINT` 和 `SIGTERM`，收到信号后取消操作并清理锁。SSH 断开可能终止裸 `&` 后台任务；这不代表任务会持续运行，但重跑不会因新版残留锁而被永久阻塞。希望断开连接后继续同步，请使用 `nohup` 或 `tmux`。锁机制面向 Linux/macOS 本地文件系统；不支持锁的文件系统会明确报错。

## 登录或配置失败

- 确认 `user` 是完整邮箱地址，`password` 是[网页端生成的客户端专用密码](client-password.md)，不是专用密码名称或网页登录密码。
- 配置含密码时，执行 `chmod 600 ~/.config/hust-mail-cli/config.json`，限制其他用户读取。
- 使用 `hust-mail-cli folders` 检查只读 IMAP 连接；确认主机、端口及专用密码对应的协议访问权限。
- 总操作超时可使用 `--timeout 60m` 调整；终端输入密码的时间不计入该限制。

## POP3 时间范围报错

POP3 不提供可靠的服务器收信时间，不能使用非零 `days`。请切换为 IMAP，或使用 `--protocol pop3 --host pop.hust.edu.cn --port 995 --days 0`。

## 正文无法解码或超过限制

`read` 和 `search` 对解码后正文有 8 MiB 限制。需要查看原始内容时使用 `hust-mail-cli read --raw /path/to/message.eml`。附件导出不需要将附件内容转换为正文。
