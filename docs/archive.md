# 本地归档

完整邮件以 `.eml` 保存，包含正文和附件。账号目录按协议、服务器、端口、账号隔离；IMAP 再按 base64url 文件夹名和 UIDVALIDITY 分目录。POP3 以 UIDL 的 SHA-256 作为文件名，要求服务器支持稳定的 UIDL。

重复同步只下载缺失或零字节文件；服务器删除邮件不会删除本地归档。IMAP 的 UIDVALIDITY 改变时保留旧目录并新建目录，可能产生内容重复。IMAP 与 POP3 使用独立目录，同一封邮件通过两种协议同步会各存一份。

目录权限为 `0700`，邮件文件为 `0600`。下载先写临时文件，成功后才发布最终文件；失败可重跑。工具不会校验已有非空文件的完整性，也不提供归档加密。

归档默认保存在 `~/HUST-Mail/`，可以通过配置的 `dir` 或同步命令的 `--dir` 修改。[同步时间范围](command-reference.md#时间范围)只限制新增下载，不会清理已归档邮件。

读取和搜索请参考 [list](command-reference.md#list-列出本地邮件)、[read](command-reference.md#read-阅读本地邮件) 和 [search](command-reference.md#search-搜索本地邮件)。
