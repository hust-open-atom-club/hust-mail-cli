# attachments 列出和导出附件

对指定本地 `.eml` 操作，不读取账号配置，不连接邮箱，不修改原始邮件。

## 列出附件

```sh
hust-mail-cli attachments list /path/to/message.eml
```

按 JSON Lines 输出附件 `index`、`filename`、`content_type`、`size`。编号从 1 开始，包含带文件名的内联资源，如图片；大小为传输解码后的字节数。

## 导出附件

```sh
# 导出所有附件到默认目录 ./attachments
hust-mail-cli attachments export /path/to/message.eml

# 指定输出目录
hust-mail-cli attachments export /path/to/message.eml --output ~/Downloads/mail-attachments

# 只导出第 2 个附件
hust-mail-cli attachments export /path/to/message.eml --index 2 --output ./attachments
```

`--index 0` 或省略表示导出全部附件。`--output` 默认 `./attachments`，相对于当前工作目录，支持开头的 `~/` 展开。

## 文件内容与覆盖保护

附件只解码 Base64、Quoted-Printable 等传输编码，保留原始字符集字节；GBK 等文本附件不会转换为 UTF-8。列表大小与实际导出字节数一致。

导出名称带 `001-` 等编号前缀，移除原始文件名中的目录路径，文件权限为 `0600`。不会覆盖已有文件或符号链接；解码失败时清理临时文件。遇到重名冲突请改用新的输出目录。


返回[命令参考](../command-reference.md)。
