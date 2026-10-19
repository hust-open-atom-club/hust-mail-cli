# list 列出本地邮件

遍历本地归档，按 JSON Lines 输出邮件头。不会连接邮箱。

```sh
hust-mail-cli list
hust-mail-cli list --dir ~/HUST-Mail/
```

`--dir` 覆盖配置中的归档目录，未配置时使用 `~/HUST-Mail/`。输出字段为 `path`、`subject`、`from`、`date`；主题会解码，发件人保留原始头字段值。`path` 可用于后续读取：

```sh
hust-mail-cli read /path/to/message.eml
```

本地归档结构见[本地归档](../archive.md)。正文搜索使用 [search](search.md)，此命令不提供筛选或排序参数。


返回[命令参考](../command-reference.md)。
