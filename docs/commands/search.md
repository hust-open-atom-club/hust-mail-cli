# search 搜索本地邮件

按不区分大小写的字面子串搜索主题、发件人、收件人和正文，不连接邮箱。

```sh
hust-mail-cli search "会议"
hust-mail-cli search --from "example.com" --subject "通知" --limit 20
hust-mail-cli search --body "项目进展" --dir ~/HUST-Mail/
```

## 参数

| 参数 | 说明 |
| --- | --- |
| `[query]` | 可选的一个搜索词，匹配主题、发件人、收件人或正文 |
| `--subject` | 主题子串 |
| `--from` | 发件人子串 |
| `--to` | 收件人子串 |
| `--body` | 解码后正文子串 |
| `--dir` | 覆盖配置中的归档目录 |
| `--limit` | 最大结果数，默认 `0` 表示不限，不能为负数 |

至少提供搜索词或一个字段条件。各条件之间取 AND，不支持正则表达式。正文与 [read](read.md) 使用相同的文本选择规则，不搜索附件内容。

## 输出和限制

输出为 JSON Lines，字段包含绝对路径 `path`、`subject`、`from`、`to`、`date` 和附件数量 `attachments`。没有匹配时输出为空，成功退出。

搜索逐封扫描 `.eml`，不建立索引；遇到无法解析的邮件或正文超过 8 MiB 限制时会报错。结果按归档遍历顺序输出，`--limit` 不代表按日期取最新邮件。


返回[命令参考](../command-reference.md)。
