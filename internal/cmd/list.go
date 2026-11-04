package cmd

import (
	"fmt"
	"os"
	"sort"

	s "github.com/gdrte/totp-go/internal/storage"
	"github.com/gdrte/totp-go/internal/terminal"
	"github.com/urfave/cli/v2"
)

// ListCommand is the list subcommand.
func ListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List all available namespaces or accounts under a namespace.",
		ArgsUsage: "[namespace]",
		Action: func(ctx *cli.Context) error {
			term := terminal.New(os.Stdin, os.Stdout, os.Stderr)
			storage := prepareStorage(term)

			err := storage.Prepare()
			if err != nil {
				return err
			}

			return executeList(storage, ctx.Args().Get(argSetPrefixPositionNamespace))
		},
	}
}

func executeList(storage s.Storage, ns string) error {
	if len(ns) < 1 {
		for _, namespace := range storage.ListNamespaces() {
			fmt.Printf("%s (Number of accounts: %d)\n", namespace.Name, len(namespace.Accounts))
		}

		return nil
	}

	namespace, err := storage.FindNamespace(ns)
	if err != nil {
		return err
	}

	sort.Slice(namespace.Accounts, func(i, j int) bool {
		return namespace.Accounts[i].Name < namespace.Accounts[j].Name
	})

	for _, account := range namespace.Accounts {
		fmt.Printf("%s.%s\n", namespace.Name, account.Name)
	}

	return nil
}
