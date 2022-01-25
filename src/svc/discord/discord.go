package discord

import (
	"fmt"
	"strings"
	"sync"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/instance"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

type discordInstsnce struct {
	discord *discordgo.Session
	gCtx    global.Context
	done    chan struct{}
	cmdsMtx sync.Mutex
	cmds    map[string]command.Cmd
}

func New(gCtx global.Context) instance.Discord {
	discord, err := discordgo.New(fmt.Sprintf("Bot %s", gCtx.Config().Discord.Token))
	if err != nil {
		logrus.Fatal("failed to make discord bot: ", err)
	}

	d := &discordInstsnce{
		discord: discord,
		gCtx:    gCtx,
		done:    make(chan struct{}),
		cmds:    map[string]command.Cmd{},
	}

	discord.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsAll)

	discord.AddHandler(d.messageCreate)

	if err := discord.Open(); err != nil {
		logrus.Fatal("failed to open discord bot: ", err)
	}

	go func() {
		<-gCtx.Done()
		if err := discord.Close(); err != nil {
			logrus.Error("failed to shutdown discord bot: ", err)
		}
		close(d.done)
	}()

	return d
}

func (d *discordInstsnce) AddHandler(handler interface{}) func() {
	return d.discord.AddHandler(handler)
}

func (d *discordInstsnce) RegisterCommand(prefix string, cmd command.Cmd) error {
	d.cmdsMtx.Lock()
	defer d.cmdsMtx.Unlock()

	prefix = strings.ToLower(prefix)

	if _, ok := d.cmds[prefix]; ok {
		return command.ErrCommandAlreadyExists
	}

	d.cmds[prefix] = cmd
	return nil
}

func (d *discordInstsnce) DeregisterCommand(prefix string, cmd command.Cmd) error {
	d.cmdsMtx.Lock()
	defer d.cmdsMtx.Unlock()

	prefix = strings.ToLower(prefix)

	if v, ok := d.cmds[prefix]; !ok || v != cmd {
		return command.ErrCommandDoesNotExist
	}

	delete(d.cmds, prefix)
	return nil
}

func (d *discordInstsnce) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot || m.GuildID != d.gCtx.Config().Discord.GuildID {
		return
	}

	if !strings.HasPrefix(m.Content, "!") {
		return
	}

	path := strings.Split(m.Content[1:], " ")

	d.cmdsMtx.Lock()
	defer d.cmdsMtx.Unlock()
	if cmd, ok := d.cmds[strings.ToLower(path[0])]; ok {
		if cmd.Match(path) {
			err := cmd.Execute(s, m, path[1:])
			switch err {
			case command.ErrCommandNotFound:
				logrus.Info("command not found")
			case nil:
				return
			default:
				logrus.Info("internal server error: ", err)
			}
		}
	}
}
