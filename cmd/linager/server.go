package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/server"
	"github.com/bolgov0zero/linager/server/store"
	"github.com/bolgov0zero/linager/web"
)

const defaultServerConfig = "/etc/linager/server.yaml"

func newServerCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{Use: "server", Short: "Linager server"}
	cmd.PersistentFlags().StringVar(&cfgPath, "config", defaultServerConfig, "path to server.yaml")

	cmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the server (admin panel and agent port)",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.LoadServer(cfgPath)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			static, err := fs.Sub(web.Dist, "dist")
			if err != nil {
				return err
			}
			return server.Run(ctx, cfg, static, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create missing keys and certificates in the data directory",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.LoadServer(cfgPath)
			if err != nil {
				return err
			}
			return server.InitKeys(cfg.DataDir, cfg.Agents.Hosts)
		},
	})

	var login, pwFile string
	create := &cobra.Command{
		Use:   "create-admin",
		Short: "Create the administrator account",
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := config.LoadServer(cfgPath)
			if err != nil {
				return err
			}
			pw, err := readSecretFile(pwFile)
			if err != nil {
				return err
			}
			db, err := store.Open(c.Context(), cfg.Database.DSN)
			if err != nil {
				return err
			}
			defer db.Close()
			return server.CreateAdmin(c.Context(), db, login, pw)
		},
	}
	create.Flags().StringVar(&login, "login", "", "administrator login")
	create.Flags().StringVar(&pwFile, "password-file", "", "file with the password ('-' for stdin)")
	_ = create.MarkFlagRequired("login")
	_ = create.MarkFlagRequired("password-file")
	cmd.AddCommand(create)
	return cmd
}

func readSecretFile(path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = readAll(os.Stdin)
	} else {
		b, err = os.ReadFile(path) //nolint:gosec // operator-supplied path
	}
	if err != nil {
		return "", err
	}
	pw := strings.TrimRight(string(b), "\r\n")
	if pw == "" {
		return "", fmt.Errorf("password file is empty")
	}
	return pw, nil
}
