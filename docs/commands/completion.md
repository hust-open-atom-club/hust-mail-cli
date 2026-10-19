# completion 生成补全脚本

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


返回[命令参考](../command-reference.md)。
