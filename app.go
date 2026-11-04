package main

import (
	"fmt"
	"os"

	"github.com/gdrte/totp-go/internal/cmd"
	"github.com/gdrte/totp-go/internal/info"
	"github.com/urfave/cli/v2"
)

func newApplication() *cli.App {
	stdErr := os.Stderr

	return &cli.App{
		Name:     info.Name,
		HelpName: "totp-cli",
		Usage:    "Authy/Google Authenticator like TOTP CLI tool written in Go.",
		Version:  info.Version,
		Commands: []*cli.Command{
			cmd.AddTokenCommand(),
			cmd.ChangePasswordCommand(),
			cmd.DeleteCommand(),
			cmd.DumpCommand(),
			cmd.GenerateCommand(),
			cmd.ImportCommand(),
			cmd.InstantCommand(),
			cmd.ListCommand(),
			cmd.ServeCommand(),
			cmd.SetPrefixCommand(),
			cmd.SetLengthCommand(),
			cmd.RenameCommand(),
			cmd.UpdateCommand(),
		},
		Authors: []*cli.Author{
			{Name: "Devanand GangiReddy"},
			{Name: "Efertone (original totp-cli)", Email: "victoria@efertone.me"},
		},
		EnableBashCompletion: true,
		ExitErrHandler: func(ctx *cli.Context, err error) {
			if err == nil {
				return
			}

			_, _ = fmt.Fprintf(stdErr, " !!! %s\n", err)

			_ = cli.ShowAppHelp(ctx)
		},
		Suggest: true,
	}
}
