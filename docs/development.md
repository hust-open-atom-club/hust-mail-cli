# 开发与验证

参考 `atomgit-cli` 的命令构造方式：`cmd/hust-mail-cli` 为入口，`pkg/cmd/root` 使用 `NewCmdRoot`、`RunE`、`ExecuteContext`，`internal/mailarchive` 处理收信和本地存储。

```sh
go test -race ./...
go vet ./...
```

协议测试使用内存连接，限制实际发送的命令，并覆盖只读读取、增量同步、UIDVALIDITY 变更和下载中断。MIME 测试覆盖字符集与传输编码、正文选择、HTML 转文本、搜索过滤、异常邮件跳过、附件边界扫描和解码隔离、单次头部/正文读取、缓冲读取及取消、索引失效与重建、旧 JSON 迁移、数据库锁竞争及安全回退、时间筛选和排序限量、列表坏邮件容错，以及附件导出的路径与覆盖保护。

真实账号已验证 IMAP 的小文件夹同步、重跑及标志不变。新增本地功能已用 macOS 上 138 封真实归档验证，并在 Linux 主机上执行原生测试和归档读取检查；未进行全量邮箱同步。

## 文档组织

README 保留项目简介、安装和快速开始。命令参数与行为说明统一维护在[命令参考](command-reference.md)中；配置、归档和故障恢复分别维护。新增或修改命令时同步更新命令参考中的对应章节。

常用开发目标与交叉构建方法见[安装与构建](installation.md)。

## 搜索性能基准

```sh
go test ./internal/mailarchive -run '^$' -bench BenchmarkSearchArchive -benchtime=3x
```

基准使用 32 封含附件的合成邮件，对比无索引正文搜索、首次重建索引、已建立索引后的正文搜索和无索引邮件头搜索。它用于比较本机行为，不代表真实邮箱性能；首次建索引还有解析和写入成本。

```sh
go test ./internal/mailarchive -run '^$' -bench BenchmarkSearchCacheLookup -benchtime=5x -count=3
```

该基准用同一组 1,024 条合成缓存记录对比旧版逐文件 JSON 和单文件数据库读取，包含源文件校验；它不代表真实归档的整体查询耗时。
