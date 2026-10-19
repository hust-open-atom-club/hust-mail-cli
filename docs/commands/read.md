# read 阅读本地邮件

```sh
hust-mail-cli read /path/to/message.eml
hust-mail-cli read --raw /path/to/message.eml
```

必须提供一个 `.eml` 路径；可通过 [list](list.md) 或 [search](search.md) 的输出获得。此命令不读取账号配置，不连接邮箱，也不修改原始文件。

## 正文展示

默认解码 MIME 正文和字符集，展示邮件头与可读正文；优先使用纯文本，仅有 HTML 时转换为文本。不会执行 HTML、脚本或加载外部资源，展示时过滤终端控制字符。

每封邮件最多解析 8 MiB 解码后正文；超限或无法解码时明确报错。

## 原始输出

`--raw` 输出原始 MIME 字节，保留旧版 `read` 的行为。可重定向到其他本地文件：

```sh
hust-mail-cli read --raw /path/to/message.eml > ./message-copy.eml
```

原始输出不进行正文展示的控制字符过滤。单独导出附件请使用 [attachments export](attachments.md#导出附件)。


返回[命令参考](../command-reference.md)。
