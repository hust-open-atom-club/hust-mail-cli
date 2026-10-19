# 命令参考

运行 `hust-mail-cli --help` 查看命令概览，使用 `hust-mail-cli <command> --help` 查看对应参数。示例使用已安装的命令；尚未安装时，在仓库根目录使用 `./bin/hust-mail-cli`。

| 命令 | 功能 | 是否连接邮箱 |
| --- | --- | --- |
| [sync](commands/sync.md) | 增量同步邮件到本地归档 | 是，只读 |
| [folders](commands/folders.md) | 列出可读取的 IMAP 文件夹 | 是，只读 |
| [list](commands/list.md) | 输出本地邮件头列表 | 否 |
| [read](commands/read.md) | 展示正文或输出原始邮件 | 否 |
| [search](commands/search.md) | 搜索本地邮件头与正文 | 否 |
| [attachments](commands/attachments.md) | 列出附件或导出到指定目录 | 否 |
| [completion](commands/completion.md) | 生成 shell 补全脚本 | 否 |

## 通用选项与配置

`--config /path/to/config.json` 指定配置文件，默认路径为 `~/.config/hust-mail-cli/config.json`。需要配置的命令遵循显式参数、支持的环境变量、配置文件、内置默认值的优先级。帮助、补全、`read` 和附件命令不读取账号配置；`list`、`search` 从配置解析归档目录，但不连接邮箱。

只读收信的连接选项、密码输入方式和权限要求见[配置指南](configuration.md)。SMTP 暂未实现；没有发送、回复、转发、删除或 doctor 子命令。
