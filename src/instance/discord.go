package instance

import (
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/bwmarrin/discordgo"
)

type Discord interface {
	RegisterCommand(prefix string, cmd command.Cmd) error
	DeregisterCommand(prefix string, cmd command.Cmd) error
	SendMessage(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error)
	SendPrivateMessage(userID string, msg *discordgo.MessageSend) (*discordgo.Message, error)
	Member(guildID string, userID string) (*discordgo.Member, error)
	AddHandler(handler interface{}) func()
}
