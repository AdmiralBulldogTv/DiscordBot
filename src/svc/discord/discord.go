package discord

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/instance"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/writer"
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

	d.initLogger()

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

func (d *discordInstsnce) SendMessage(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	return d.discord.ChannelMessageSendComplex(channelID, msg)
}

func (d *discordInstsnce) SendPrivateMessage(userID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	st, err := d.discord.UserChannelCreate(userID)
	if err != nil {
		logrus.Error("failed to create dm channel: ", err)
		return nil, err
	}

	return d.SendMessage(st.ID, msg)
}

func (d *discordInstsnce) Member(guildID string, userID string) (*discordgo.Member, error) {
	return d.discord.GuildMember(guildID, userID)
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

func (d *discordInstsnce) initLogger() {
	if !d.gCtx.Config().Discord.Logging.Enabled {
		return
	}

	rErr, wErr := io.Pipe()
	go d.handleLogStream(rErr)
	logrus.AddHook(&writer.Hook{
		Writer: wErr,
		LogLevels: []logrus.Level{
			logrus.PanicLevel,
			logrus.FatalLevel,
			logrus.ErrorLevel,
			logrus.WarnLevel,
		},
	})
	if d.gCtx.Config().Discord.Logging.Debug {
		rStd, wStd := io.Pipe()
		go d.handleLogStream(rStd)
		logrus.AddHook(&writer.Hook{
			Writer: wStd,
			LogLevels: []logrus.Level{
				logrus.InfoLevel,
				logrus.DebugLevel,
				logrus.TraceLevel,
			},
		})
	}

	logrus.Info("discord logging init")
}

func (d *discordInstsnce) handleLogStream(logs io.Reader) {
	lines := textproto.NewReader(bufio.NewReader(logs))
	buf := bytes.Buffer{}
	mtx := sync.Mutex{}
	go func() {
		tick := time.NewTicker(time.Second)
		for range tick.C {
			mtx.Lock()
			line := strings.TrimSpace(buf.String())
			if len(line) != 0 {
				if _, err := d.discord.ChannelMessageSend(d.gCtx.Config().Discord.Logging.ChannelID, buf.String()); err != nil {
					logrus.Error("failed to send chat message: ", err)
				}
			}
			buf.Reset()
			mtx.Unlock()
		}

	}()
	for {
		line, err := lines.ReadLine()
		if err != nil {
			logrus.Fatal("failed to read line: ", err)
		}

		mtx.Lock()
		if buf.Len()+len(line)+1 > 1800 {
			// we need to flush the buffer now.
			line := strings.TrimSpace(buf.String())
			if len(line) != 0 {
				if _, err := d.discord.ChannelMessageSend(d.gCtx.Config().Discord.Logging.ChannelID, line); err != nil {
					logrus.Error("failed to send chat message: ", err)
				}
			}
			buf.Reset()
		}
		_, _ = buf.WriteString(line + "\n")
		mtx.Unlock()
	}
}
