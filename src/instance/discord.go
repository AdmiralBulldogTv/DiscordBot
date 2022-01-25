package instance

import "github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"

type Discord interface {
	RegisterCommand(prefix string, cmd command.Cmd) error
	DeregisterCommand(prefix string, cmd command.Cmd) error
	AddHandler(handler interface{}) func()
}
