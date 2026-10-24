package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"

	"github.com/hust-open-atom-club/hust-mail-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type configPrompt func(context.Context, *cobra.Command) (config.Config, error)

func newConfigCommand() *cobra.Command { return newConfigCommandWithPrompt(promptConfig) }
func newConfigCommandWithPrompt(prompt configPrompt) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage local HUST mail configuration"}
	init := &cobra.Command{Use: "init", Short: "Interactively create a private config without overwriting an existing file", Args: cobra.NoArgs}
	init.RunE = func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		path, err := config.ResolvePath(path)
		if err != nil {
			return err
		}
		if _, err = os.Lstat(path); err == nil {
			return errors.New("config already exists; edit it manually (not overwritten)")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		cfg, err := prompt(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if err := config.Create(path, cfg); err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Path   string `json:"path"`
				Status string `json:"status"`
			}{path, "created"})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "配置已创建：%s\n可在配置文件中设置其他选项：days（默认 0，全部历史）、mailbox（默认全部文件夹）、timeout（默认 30m）。\n运行 hust-mail-cli doctor --timeout 15s 检查连接。\n", path)
		return err
	}
	cmd.AddCommand(init)
	return cmd
}

// terminalIO polls the dedicated terminal so cancellation restores its state
// promptly even while waiting for a line or a hidden password.
type terminalIO struct {
	ctx  context.Context
	file *os.File
	fd   int
}

func (t terminalIO) Read(p []byte) (int, error) {
	for {
		if err := t.ctx.Err(); err != nil {
			return 0, err
		}
		fds := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n > 0 {
			n, err := unix.Read(t.fd, p)
			if err == unix.EAGAIN || err == unix.EINTR {
				continue
			}
			if n == 0 && err == nil {
				return 0, io.EOF
			}
			return max(n, 0), err
		}
	}
}
func (t terminalIO) Write(p []byte) (int, error) { return t.file.Write(p) }

func promptConfig(ctx context.Context, cmd *cobra.Command) (config.Config, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return config.Config{}, errors.New("config init requires an interactive terminal; create config.json manually using config.example.json")
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return config.Config{}, errors.New("cannot prepare interactive terminal")
	}
	defer term.Restore(fd, state)
	// Poll readiness can be transient (notably on terminal signal delivery).
	// A nonblocking syscall prevents a subsequent read from defeating cancellation.
	if err := unix.SetNonblock(fd, true); err != nil {
		return config.Config{}, errors.New("cannot prepare cancellable terminal input")
	}
	rw := terminalIO{ctx: ctx, file: tty, fd: fd}
	terminal := term.NewTerminal(rw, "")
	var cfg config.Config
	for {
		terminal.SetPrompt("邮箱地址：")
		value, err := terminal.ReadLine()
		if err != nil {
			return cfg, errors.New("configuration input canceled")
		}
		value = strings.TrimSpace(value)
		addr, err := mail.ParseAddress(value)
		if err == nil && addr.Address == value && strings.Contains(value, "@") && !strings.ContainsAny(value, "\r\n") {
			cfg.User = value
			break
		}
		fmt.Fprintln(terminal, "请输入完整邮箱地址，例如 your-name@hust.edu.cn。")
	}
	for {
		password, err := terminal.ReadPassword("客户端专用密码（网页端生成，输入隐藏）：")
		if err != nil {
			return cfg, errors.New("configuration input canceled")
		}
		if password != "" {
			cfg.Password = password
			break
		}
		fmt.Fprintln(terminal, "专用密码不能为空。")
	}
	terminal.SetPrompt("邮件归档目录 [~/HUST-Mail/]：")
	dir, err := terminal.ReadLine()
	if err != nil {
		return cfg, errors.New("configuration input canceled")
	}
	cfg.Dir = strings.TrimSpace(dir)
	if cfg.Dir == "" {
		cfg.Dir = config.DefaultArchiveDir
	}
	if strings.ContainsAny(cfg.Dir, "\r\n\x00") {
		return cfg, errors.New("invalid archive directory")
	}
	return cfg, nil
}
