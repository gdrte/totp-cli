package main

import (
	"github.com/yitsushi/go-commander"
	"github.com/gdrte/totp-go/command"
)

func registerCommands(registry *commander.CommandRegistry) {
	// Register available commands
	registry.Register(command.NewGenerate)
	registry.Register(command.NewAddToken)
	registry.Register(command.NewList)
	registry.Register(command.NewDelete)
	registry.Register(command.NewChangePassword)
	registry.Register(command.NewVersion)
	registry.Register(command.NewUpdate)
}

func main() {
	registry := commander.NewCommandRegistry()

	registerCommands(registry)

	registry.Execute()
}
