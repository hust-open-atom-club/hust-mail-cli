# 命令参考

运行 `hust-mail-cli --help` 查看命令概览，使用 `hust-mail-cli <command> --help` 查看对应参数。示例使用已安装的命令；尚未安装时，在仓库根目录使用 `./bin/hust-mail-cli`。

| 命令 | 功能 | 是否连接邮箱 |
| --- | --- | --- |
| [sync](command-reference.md#sync-同步邮件) | 增量同步邮件到本地归档 | 是，只读 |
| [folders](command-reference.md#folders-列出文件夹) | 列出可读取的 IMAP 文件夹 | 是，只读 |
| [list](command-reference.md#list-列出本地邮件) | 输出本地邮件头列表 | 否 |
| [read](command-reference.md#read-阅读本地邮件) | 展示正文或输出原始邮件 | 否 |
| [search](command-reference.md#search-搜索本地邮件) | 搜索本地邮件头与正文 | 否 |
| [attachments](command-reference.md#attachments-列出和导出附件) | 列出附件或导出到指定目录 | 否 |
| [completion](command-reference.md#completion-生成补全脚本) | 生成 shell 补全脚本 | 否 |

## 通用选项与配置

`--config /path/to/config.json` 指定配置文件，默认路径为 `~/.config/hust-mail-cli/config.json`。需要配置的命令遵循显式参数、支持的环境变量、配置文件、内置默认值的优先级。帮助、补全、`read` 和附件命令不读取账号配置；`list`、`search` 从配置解析归档目录，但不连接邮箱。

`--json` 是全局选项，可放在命令前后，包括 `attachments` 的子命令。业务命令的结果输出为 JSON Lines（每行一个 JSON 对象），方便管道处理；进度、警告和错误写入标准错误。帮助与 shell 补全仍输出原有文本/脚本。失败时退出码非零；没有结果的列表可以没有输出。

```sh
hust-mail-cli --json sync --quiet
hust-mail-cli folders --json
hust-mail-cli read /path/to/message.eml --json
hust-mail-cli attachments list /path/to/message.eml --json
```

`list`、`search`、`attachments list/export` 原本就输出 JSON Lines，加上 `--json` 后字段与格式保持一致。`read --json` 输出 `path`、`subject`、`from`、`to`、`date`、`body` 和附件元数据数组 `attachments`；无附件时为 `[]`。`read --raw` 与 `--json` 不可同时使用。

只读收信的连接选项、密码输入方式和权限要求见[配置指南](configuration.md)。SMTP 暂未实现；没有发送、回复、转发、删除或 doctor 子命令。

## sync 同步邮件

只读连接邮箱，将缺失邮件保存为本地 `.eml`，包含正文和附件。默认使用 IMAP，同步所有可读取文件夹。

```sh
hust-mail-cli sync
hust-mail-cli sync --mailbox INBOX
hust-mail-cli sync --dir ~/HUST-Mail/
hust-mail-cli sync --days 7
hust-mail-cli sync --days 0
```

### 同步选项

| 参数 | 默认行为 | 说明 |
| --- | --- | --- |
| `--mailbox` | 所有可读取文件夹 | 指定一个 IMAP 文件夹；名称可先用 `folders` 查询 |
| `--dir` | `~/HUST-Mail/` | 本地归档目录，支持开头的 `~/` 展开 |
| `--days` | `0`，不限时间 | 新增下载的时间范围，可由配置覆盖；示例配置为 `7` |
| `--quiet` | `false` | 关闭实时进度，保留文件夹汇总、警告和错误 |
| `--retry-ignored` | `false` | 重试此前记录为正文不可用的 IMAP 邮件 |

连接选项 `--user`、`--host`、`--protocol`、`--port`、`--timeout` 和全局 `--config` 见[配置指南](configuration.md)。显式参数覆盖配置。

### 时间范围

`--days 7` 表示本次同步启动前最近 7×24 小时，`--days 0` 明确覆盖配置并下载全部时间的邮件。允许值为 0–36500。

筛选依据是 IMAP 服务端 `INTERNALDATE`，不是发件人提供的 `Date`。服务器日期搜索适当放宽候选范围，再逐封核验精确收信时间；缺失收信时间时会报错。旧邮件不会下载正文，已归档文件不会因超过期限被自动删除。

### 增量与只读保证

使用 `EXAMINE` 和 `BODY.PEEK[]`，不标记已读，不修改、删除或上传远端邮件。重复运行只下载缺失或零字节文件；归档标识与 UIDVALIDITY 行为见[本地归档](archive.md)。每个文件夹输出一行，按列对齐文件夹名称及 `DOWNLOADED`、`EXISTING`、`IGNORED` 计数（包括零值），最后输出下载总数和归档位置，例如：

```text
FOLDER        DOWNLOADED    EXISTING     IGNORED
"INBOX"                0       13663           8
"Sent Items"           0         388           5
```

`sync --json` 按文件夹逐行输出 `type: "folder"` 记录，最后输出 `type: "complete"` 的总计记录：

```json
{"type":"folder","folder":"INBOX","downloaded":0,"existing":13663,"ignored":8}
{"type":"complete","downloaded":0,"existing":13663,"ignored":8,"archive":"/path/to/archive"}
```

总计包含本次所有文件夹的计数；有时间筛选时还带 `cutoff`（RFC3339 格式）。中途失败时可能已输出部分文件夹结果，不会输出最终总计记录；请同时检查退出码。JSON 模式下，时间范围提示写入标准错误。

中断后可直接重跑；SSH 后台运行和残留锁处理见[同步恢复](troubleshooting.md)。

服务器不返回个别邮件正文时，将其作为警告跳过，继续同步其余邮件和文件夹，最后显示 `ignored` 数量并正常退出。确认邮件仍存在但正文不可用时，会持久化忽略记录；后续同步直接跳过，不再下载或重复告警。`ignored` 仍包含这些记录，不计入下载数量，也不代表邮件已备份。详情见[故障排查](troubleshooting.md#邮件仍存在但服务器不返回正文)。

需要重新尝试这些邮件时，显式运行：

```sh
hust-mail-cli sync --retry-ignored --days 0
```

重试仅针对本次选中的文件夹和时间范围，成功后清除对应忽略记录；失败时保留记录。此选项仅适用于 IMAP。

### 实时进度

默认向标准错误输出进度。交互式终端显示原地刷新的两行进度条，例如：

```text
Folder 1/8 "INBOX" [████████░░░░░░░░░░░░]  40% 400/1000 UID=12345
new=100 existing=299 ignored=1 filtered=0 rx=120.0 MiB time=2m30s
```

显示当前文件夹序号、候选邮件处理比例、正在处理的 UID、下载及跳过数量、本文件夹累计接收字节数和本次同步已用时。已有文件也计入处理进度；`filtered` 表示候选邮件经精确收信时间检查后被排除。百分比按当前文件夹的候选邮件数量计算，不是字节比例，也不是整个邮箱的完成比例。

连接、认证、列举文件夹和搜索期间显示当前阶段；大邮件传输及等待响应时，进度会持续刷新。重定向或 `nohup` 运行时自动改为普通日志，阶段切换及文件夹完成时立即输出，其余时间每 5 秒输出一次，不含终端控制字符。文件夹与最终汇总仍在标准输出。

```sh
# 日志同时记录进度和汇总
hust-mail-cli sync --days 0 > ~/hust-mail-sync.log 2>&1

# 关闭进度，但保留警告、错误和汇总
hust-mail-cli sync --quiet
```

## folders 列出文件夹

只读连接 IMAP 邮箱，列出可选择的文件夹，便于确定 `sync --mailbox` 的名称。

```sh
hust-mail-cli folders
hust-mail-cli folders --user 你的邮箱@hust.edu.cn
hust-mail-cli folders --config /path/to/config.json
```

输出每行一个带引号的文件夹名称，例如 `"INBOX"`。`folders --json` 则输出 `{"folder":"INBOX"}` 形式的 JSON Lines。将实际名称传给同步命令：

```sh
hust-mail-cli sync --mailbox INBOX
```

支持 `--user`、`--host`、`--protocol`、`--port` 和 `--timeout`；连接与凭据规则见[配置指南](configuration.md)。此命令仅适用于 IMAP，不下载正文，不修改远端邮件，也不创建本地归档。

## list 列出本地邮件

遍历本地归档，按 JSON Lines 输出邮件头。不会连接邮箱。

```sh
hust-mail-cli list
hust-mail-cli list --dir ~/HUST-Mail/
```

`--dir` 覆盖配置中的归档目录，未配置时使用 `~/HUST-Mail/`。输出字段为 `path`、`subject`、`from`、`date`；主题会解码，发件人保留原始头字段值。`path` 可用于后续读取：

```sh
hust-mail-cli read /path/to/message.eml
```

本地归档结构见[本地归档](archive.md)。正文搜索使用 [search](command-reference.md#search-搜索本地邮件)，此命令不提供筛选或排序参数。

## read 阅读本地邮件

```sh
hust-mail-cli read /path/to/message.eml
hust-mail-cli read --raw /path/to/message.eml
```

必须提供一个 `.eml` 路径；可通过 [list](command-reference.md#list-列出本地邮件) 或 [search](command-reference.md#search-搜索本地邮件) 的输出获得。此命令不读取账号配置，不连接邮箱，也不修改原始文件。

### 正文展示

默认解码 MIME 正文和字符集，展示邮件头与可读正文；优先使用纯文本，仅有 HTML 时转换为文本。不会执行 HTML、脚本或加载外部资源，展示时过滤终端控制字符。

每封邮件最多解析 8 MiB 解码后正文；超限或无法解码时明确报错。

### 原始输出

`--raw` 输出原始 MIME 字节，保留旧版 `read` 的行为。可重定向到其他本地文件：

```sh
hust-mail-cli read --raw /path/to/message.eml > ./message-copy.eml
```

原始输出不进行正文展示的控制字符过滤。单独导出附件请使用 [attachments export](command-reference.md#导出附件)。

## search 搜索本地邮件

按不区分大小写的字面子串搜索主题、发件人、收件人和正文，不连接邮箱。

```sh
hust-mail-cli search "会议"
hust-mail-cli search --from "example.com" --subject "通知" --limit 20
hust-mail-cli search --body "项目进展" --dir ~/HUST-Mail/
```

### 参数

| 参数 | 说明 |
| --- | --- |
| `[query]` | 可选的一个搜索词，匹配主题、发件人、收件人或正文 |
| `--subject` | 主题子串 |
| `--from` | 发件人子串 |
| `--to` | 收件人子串 |
| `--body` | 解码后正文子串 |
| `--dir` | 覆盖配置中的归档目录 |
| `--limit` | 最大结果数，默认 `0` 表示不限，不能为负数 |

至少提供搜索词或一个字段条件。各条件之间取 AND，不支持正则表达式。正文与 [read](command-reference.md#read-阅读本地邮件) 使用相同的文本选择规则，不搜索附件内容。

### 输出和限制

输出为 JSON Lines，字段包含绝对路径 `path`、`subject`、`from`、`to`、`date` 和附件数量 `attachments`。没有匹配时输出为空，成功退出。

搜索逐封扫描 `.eml`，不建立索引；遇到无法解析的邮件或正文超过 8 MiB 限制时会报错。结果按归档遍历顺序输出，`--limit` 不代表按日期取最新邮件。

## attachments 列出和导出附件

对指定本地 `.eml` 操作，不读取账号配置，不连接邮箱，不修改原始邮件。

### 列出附件

```sh
hust-mail-cli attachments list /path/to/message.eml
```

按 JSON Lines 输出附件 `index`、`filename`、`content_type`、`size`。编号从 1 开始，包含带文件名的内联资源，如图片；大小为传输解码后的字节数。

### 导出附件

```sh
# 导出所有附件到默认目录 ./attachments
hust-mail-cli attachments export /path/to/message.eml

# 指定输出目录
hust-mail-cli attachments export /path/to/message.eml --output ~/Downloads/mail-attachments

# 只导出第 2 个附件
hust-mail-cli attachments export /path/to/message.eml --index 2 --output ./attachments
```

`--index 0` 或省略表示导出全部附件。`--output` 默认 `./attachments`，相对于当前工作目录，支持开头的 `~/` 展开。

### 文件内容与覆盖保护

附件只解码 Base64、Quoted-Printable 等传输编码，保留原始字符集字节；GBK 等文本附件不会转换为 UTF-8。列表大小与实际导出字节数一致。

导出名称带 `001-` 等编号前缀，移除原始文件名中的目录路径，文件权限为 `0600`。不会覆盖已有文件或符号链接；解码失败时清理临时文件。遇到重名冲突请改用新的输出目录。

## completion 生成补全脚本

使用 Cobra 提供的补全命令生成当前 shell 的脚本，不连接邮箱或读取账号配置。

```sh
hust-mail-cli completion zsh
hust-mail-cli completion bash
hust-mail-cli completion fish
```

脚本输出到标准输出。以下示例在当前 Bash 会话加载补全：

```sh
source <(hust-mail-cli completion bash)
```

Zsh 可将脚本保存到 `$fpath` 中的目录，并通过 `compinit` 加载。具体选项和安装说明见 `hust-mail-cli completion <shell> --help`。
