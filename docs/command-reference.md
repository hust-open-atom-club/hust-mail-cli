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

单封邮件无法读取或邮件头损坏时，输出警告到 stderr 并继续，结束时输出 `List: scanned=... listed=... skipped=...`。stdout 保持纯 JSON Lines，个别邮件跳过时退出成功；目录访问、取消或结果写入失败仍返回非零。`list` 不读取或建立搜索缓存，正文损坏不会阻止列出正常邮件头。

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

每封邮件最多解析 8 MiB 解码后正文。缺失或空媒体类型按默认文本处理；只有参数而没有媒体类型时补齐类型，带文件名或附件 disposition 的段仍作为二进制附件保留。无法识别的正文 charset（如 `a`、`yes`、`y`）回退 UTF-8：正常 UTF-8/ASCII 内容保留，非法字节用替换符显示；不会据此猜测 GBK 等其他编码，也不改动原始邮件或附件。

无法恢复的 MIME 结构、传输编码、正文超限或文件读取错误仍会报错，`read` 退出码为非零。恢复成功则返回 `0`。通过管道运行时，Bash/zsh 默认 `$?` 通常是最后一个命令的状态；需要检查 CLI 错误时可直接执行命令，或启用 `set -o pipefail`。

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
hust-mail-cli search "会议" --rebuild-index
hust-mail-cli search --subject "通知" --no-index
hust-mail-cli search --body bpf --after 2026-10-01 --before 2026-11-01
hust-mail-cli search --sort newest --limit 20
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
| `--no-index` | 不读取或写入索引，直接搜索原始邮件，适用于只读归档 |
| `--after` | `Date` 大于等于此时间，支持 `YYYY-MM-DD`（UTC 零点）或 RFC3339 |
| `--before` | `Date` 严格早于此时间，格式同 `--after` |
| `--sort` | `none`（默认遍历顺序）、`newest`（日期降序）、`oldest`（日期升序） |
| `--quiet` | 关闭周期进度，保留警告和失败统计 |
| `--rebuild-index` | 清除已有索引记录，并按本次查询需要重新建立；不能与 `--no-index` 同用 |

至少提供搜索词、字段条件、时间条件或显式日期排序。各条件之间取 AND，不支持正则表达式。正文与 [read](command-reference.md#read-阅读本地邮件) 使用相同的文本选择规则，不搜索附件内容。

### 输出和限制

输出为 JSON Lines，字段包含绝对路径 `path`、`subject`、`from`、`to` 和 `date`。完整正文已解析或已有正文缓存时，还包含附件数量 `attachments`；仅查邮件头且附件数未知时省略此字段，不用 `0` 代替未知值。没有匹配时输出为空，成功退出。

空媒体类型和未知正文 charset 按上述阅读规则恢复；无法恢复的 MIME 结构、正文传输编码错误或正文超过 8 MiB 时，跳过该邮件，继续搜索；警告和 `Search: scanned=... matched=... skipped=...` 失败统计写入 stderr，stdout 保持纯 JSON Lines。搜索只扫描附件的 MIME 边界并计数，不解码、校验或搜索附件载荷；附件编码损坏不会单独导致整封邮件被跳过，但破损的 MIME 边界仍可能影响后续正文。`read` 和 `attachments list/export` 继续完整解码附件并报告错误。仅查询邮件头时不检查正文，因此正文损坏的邮件仍能按主题或地址找到。目录访问失败、取消操作和结果写入失败仍导致非零退出；个别邮件被跳过时正常退出。

默认按归档遍历顺序输出，达到 `--limit` 后停止扫描，不代表最新邮件。`--sort newest --limit 20` 扫描全部候选邮件后输出最新 20 封，只保留最优的 20 条邮件头；`--sort oldest` 同理。无数量限制的日期排序会保留所有匹配邮件头，完成扫描后才输出。相同日期按路径排序；无效或缺失日期排在末尾。

时间筛选采用邮件 `Date` 头（发件人日期），不是同步使用的 IMAP `INTERNALDATE`，不代表实际收信时间。范围为 `[after, before)`，上下界同时给出时必须 `before > after`；日期格式默认 UTC，RFC3339 可明确指定时区。使用时间筛选时，无效或缺失 `Date` 会警告并跳过；未启用筛选时仍可搜索这些邮件。

### 搜索索引与性能

持续超过 5 秒的搜索，每 5 秒向 stderr 输出 `Search progress: scanned=... matched=... skipped=... cache_hits=... elapsed=...`，结束时输出最终统计。`matched` 为已扫描邮件中的全部匹配数；日期排序配合 `--limit` 时，它可以大于最终输出条数。建索引、解析大邮件或等待输出时仍可看到计数及已用时间；不预先遍历整份归档计算百分比。可用 `--quiet` 关闭进度；stdout 始终只包含 JSON Lines 结果。

默认在本次 `--dir` 对应的归档根目录下维护 `.search-index/`。它是可丢弃的解码缓存，不是独立邮件库，也不是倒排全文索引：每次仍遍历原始 `.eml`，按相对路径、大小和纳秒修改时间验证缓存；新增或变化的邮件按需解析，已删除的邮件不会从缓存返回。

仅按 `--subject`、`--from`、`--to` 搜索时只读取邮件头；组合条件先检查邮件头，不满足时不解析正文。普通搜索词已匹配邮件头且没有正文条件时，也无需读取正文。首次正文搜索复用读取邮件头的文件与解析器，不重复打开、解析同一封邮件。只有需要正文时才进行 MIME 遍历：附件按原始字节扫描边界，不进行 Base64/quoted-printable 或字符集解码，也不计算解码后的附件大小；仍需读取这些字节才能找到后续 MIME 段，并非跳过附件磁盘读取。正文遍历采用可复用的 64 KiB 底层缓冲，减少小块读取；仅查询邮件头时保持小缓冲。后续对未变化邮件复用解码文本和附件计数，避免再次解析 MIME 和读取附件。损坏正文的解析失败也会按同一文件指纹缓存，仍可单独查邮件头。

索引集中存放于 `.search-index/messages.db`，使用纯 Go 的嵌入式 bbolt，无需独立服务；每次搜索打开一次数据库，避免逐封打开 JSON 文件。旧版 JSON 缓存按需迁移，迁移后优先读取数据库，旧文件留到显式重建时清理。它仍是解码缓存，每次查询仍遍历源邮件并核验指纹，不是全文倒排索引。

目录权限为 `0700`，数据库与持久锁文件 `.index.flock` 为 `0600`。不要在搜索运行中删除锁文件。缓存最多按 128 条或约 4 MiB 批次事务写入，不逐条 fsync；断电后丢失或损坏的缓存可以重建，原始 `.eml` 的持久化写入策略不变。缓存忙、无法访问或记录损坏时警告并回退读取源邮件；数据库格式损坏时在持锁期间重新创建。不会跟随数据库的符号链接或修改硬链接目标。对只读目录可使用 `--no-index`，避免任何索引读写。

`--rebuild-index` 清空数据库记录、清理旧版 hash 命名 JSON，并按本次查询条件及 `--limit` 按需重建，不会强制解析所有正文；不修改原始邮件或目录中的无关文件。源文件若被其他工具修改却保留原大小和修改时间，需要显式重建。平时不自动删除失去源文件的缓存记录，重建时会清理它们。

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
