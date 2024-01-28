package cmd

import (
	"os"

	"github.com/urfave/cli/v2"

	"github.com/gdrte/totp-go/internal/security"
	s "github.com/gdrte/totp-go/internal/storage"
	"github.com/gdrte/totp-go/internal/terminal"
)

// ChangePasswordCommand is the change-password subcommand.
func ChangePasswordCommand() *cli.Command {
	return &cli.Command{
		Name:      "change-password",
		Usage:     "Change password.",
		ArgsUsage: "",
		Action: func(_ *cli.Context) error {
			var (
				err                  error
				newPasswordIn        string
				newPasswordConfirmIn string
			)

			storage := s.NewFileStorage()
			if err = storage.Prepare(); err != nil {
				return err
			}

			term := terminal.New(os.Stdin, os.Stdout, os.Stderr)

			if newPasswordIn, err = term.Hidden("New Password:"); err != nil {
				return err
			}

			if newPasswordConfirmIn, err = term.Hidden("Again:"); err != nil {
				return err
			}

			if !security.CheckPasswordConfirm([]byte(newPasswordIn), []byte(newPasswordConfirmIn)) {
				return CommandError{Message: "new password and the confirm mismatch"}
			}

			storage.SetPassword(newPasswordIn)

			return storage.Save()
		},
	}
}
