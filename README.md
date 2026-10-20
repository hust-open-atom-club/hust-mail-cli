# hust-mail-cli

Go 编写的华中科技大学邮箱 CLI，使用 Cobra 组织命令。默认通过只读 IMAP 收信，支持本地增量归档、搜索、正文展示和附件导出。

## 功能

| 类别 | 能力 |
| --- | --- |
| 收信 | 只读 IMAP、多文件夹增量同步、按收信时间筛选 |
| 阅读 | 本地邮件列表、解码正文展示、原始 `.eml` 输出 |
| 搜索 | 按主题、发件人、收件人和正文搜索本地邮件 |
| 附件 | 列出附件、单独导出，防止覆盖已有文件 |
| 进度与恢复 | 实时进度条、日志进度，中断后重跑并自动处理可安全回收的残留同步锁 |

业务命令均支持全局 `--json`，便于脚本处理，详见[命令参考](docs/command-reference.md#通用选项与配置)。

不修改远端邮件或已读状态。SMTP 仅预留配置，暂不支持发送、回复或转发。

## 安装

支持 Linux、macOS 及多种 CPU 架构，不考虑 Windows。需要 Go 1.26 或更新版本，在仓库根目录执行：

```sh
make
make install
```

安装到 `GOBIN`，未设置时使用 `GOPATH` 第一项的 `bin` 目录，请将安装目录加入 `PATH`。也可直接运行 `./bin/hust-mail-cli`。其他构建、安装、卸载和交叉编译方式见[安装指南](docs/installation.md)。

## 配置

先按照[客户端专用密码获取指南](docs/client-password.md)，在邮箱网页端生成专用密码，然后复制配置模板：

```sh
mkdir -p ~/.config/hust-mail-cli
cp -n config.example.json ~/.config/hust-mail-cli/config.json
chmod 700 ~/.config/hust-mail-cli
chmod 600 ~/.config/hust-mail-cli/config.json
```

填写 `user`（完整邮箱）和 `password`（专用密码）即可，服务器已预填。默认归档目录为 `~/HUST-Mail/`，模板默认只下载近 7 天邮件，不自动删除旧归档。字段、覆盖规则及安全说明见[配置指南](docs/configuration.md)。

## 使用

```sh
hust-mail-cli folders
hust-mail-cli sync
hust-mail-cli list
```

运行 `hust-mail-cli --help` 查看命令概览，或运行 `hust-mail-cli <command> --help` 查看具体参数。各命令的详细介绍和示例见[命令参考](docs/command-reference.md)。

## 更多文档

- [安装与构建](docs/installation.md)
- [配置指南](docs/configuration.md)
- [客户端专用密码获取指南](docs/client-password.md)
- [命令参考](docs/command-reference.md)
- [本地归档](docs/archive.md)
- [同步恢复与故障排查](docs/troubleshooting.md)
- [开发与验证](docs/development.md)
