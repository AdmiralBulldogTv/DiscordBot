package goodnight

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis/v8"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

type Module struct {
	done chan struct{}
	gCtx global.Context
}

func New() *Module {
	return &Module{}
}

func (m *Module) Register(gCtx global.Context) (<-chan struct{}, error) {
	m.done = make(chan struct{})
	m.gCtx = gCtx

	closeFns := []func(){}

	err := multierror.Append(nil, gCtx.Inst().Discord.RegisterCommand("gn", m.GnCmd()))
	err = multierror.Append(err, gCtx.Inst().Discord.RegisterCommand("tuck", m.TuckCmd()))
	closeFns = append(closeFns, gCtx.Inst().Discord.AddHandler(m.onMessage))

	go func() {
		<-gCtx.Done()
		for _, fn := range closeFns {
			fn()
		}
		close(m.done)
	}()

	return m.done, err
}

func (m *Module) Name() string {
	return "GoodNight"
}

func (m *Module) onMessage(s *discordgo.Session, msg *discordgo.MessageCreate) {
	if msg.GuildID != m.gCtx.Config().Discord.GuildID || msg.Author.Bot {
		return
	}

	nick := msg.Member.Nick
	if nick == "" {
		nick = msg.Author.Username
	}
	username := fmt.Sprintf("%s#%s", nick, msg.Author.Discriminator)

	ctx, cancel := context.WithTimeout(m.gCtx, time.Second*5)
	defer cancel()

	content := strings.ToLower(msg.Content)
	if strings.HasPrefix(content, "!gn") || strings.HasPrefix(content, "!tuck") {
		return
	}

	pipe := m.gCtx.Inst().Redis.Pipeline(ctx)
	getCmd := pipe.Get(ctx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
	sMembersCmd := pipe.SMembers(ctx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
	pipe.Del(ctx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
	pipe.Del(ctx, fmt.Sprintf("sleepers:%s:tucked", msg.Author.ID))
	pipe.Del(ctx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
	_, _ = pipe.Exec(ctx)

	val, err := getCmd.Result()
	if err != nil && err != redis.Nil {
		logrus.Error("failed to get key: ", err)
		return
	}
	if val != "" {
		sleepDate, _ := time.Parse(time.RFC3339, val)
		data := &discordgo.MessageSend{
			Reference: msg.Reference(),
			Content:   fmt.Sprintf("%s woke up after %s", username, (time.Since(sleepDate)/time.Second)*time.Second),
		}

		content := []string{}
		for _, v := range sMembersCmd.Val() {
			splits := strings.SplitN(v, " ", 3)
			st, err := s.ChannelMessage(splits[0], splits[1])
			if err == nil {
				at, _ := st.Timestamp.Parse()
				content = append(content, fmt.Sprintf("%s mentioned you %s ago: [here](https://discord.com/channels/%s/%s/%s)", st.Author, (time.Since(at)/time.Second)*time.Second, splits[2], st.ChannelID, st.ID))
				if len(content) == 20 {
					break
				}
			}
		}
		if len(content) != 0 {
			data.Embed = &discordgo.MessageEmbed{
				Color: s.State.MessageColor(msg.Message),
				Author: &discordgo.MessageEmbedAuthor{
					Name:    fmt.Sprintf("People who mentioned %s", msg.Author.Username),
					IconURL: msg.Author.AvatarURL(""),
				},
				Description: strings.Join(content, "\n"),
			}
		}

		if _, err := s.ChannelMessageSendComplex(msg.ChannelID, data); err != nil {
			logrus.Error("failed to send message: ", err)
		}
	}

	st, err := s.UserChannelCreate(msg.Author.ID)
	if err != nil {
		logrus.Error("failed to create dm channel: ", err)
	}

	mentions := map[string]bool{}
	for _, v := range msg.Mentions {
		if mentions[v.ID] {
			continue
		}

		mentions[v.ID] = true
		if exists, _ := m.gCtx.Inst().Redis.Exists(ctx, fmt.Sprintf("sleepers:%s", v.ID)); exists {
			perms, _ := s.UserChannelPermissions(v.ID, msg.ChannelID)
			if perms&discordgo.PermissionViewChannel != 0 {
				_ = m.gCtx.Inst().Redis.SAdd(ctx, fmt.Sprintf("sleepers:%s:mentions", v.ID), fmt.Sprintf("%s %s %s", msg.ChannelID, msg.ID, msg.GuildID))
			}

			if st != nil {
				if _, err := s.ChannelMessageSend(st.ID, fmt.Sprintf("%s is sleeping they will be notified of your ping when they wake up.", v.Mention())); err != nil {
					logrus.Error("failed to send message to user: ", err)
				}
			}
		}
	}
}

func (m *Module) GnCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "gn"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "gn")
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			if msg.Member == nil {
				var err error
				msg.Member, err = s.GuildMember(m.gCtx.Config().Discord.GuildID, msg.Author.ID)
				if err != nil {
					logrus.Errorf("failed to fetch member (%s#%s - %s): %s", msg.Author.Username, msg.Author.Discriminator, msg.Author.ID, err.Error())
					return err
				}
			}

			nick := msg.Member.Nick
			if nick == "" {
				nick = msg.Author.Username
			}
			username := fmt.Sprintf("%s#%s", nick, msg.Author.Discriminator)

			set, err := m.gCtx.Inst().Redis.SetNX(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID), time.Now().Format(time.RFC3339), 0)
			if err != nil {
				return err
			}

			if !set {
				pipe := m.gCtx.Inst().Redis.Pipeline(m.gCtx)
				getCmd := pipe.Get(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
				sMembersCmd := pipe.SMembers(m.gCtx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s:tucked", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
				_, _ = pipe.Exec(m.gCtx)

				val, _ := getCmd.Result()
				sleepDate, _ := time.Parse(time.RFC3339, val)
				data := &discordgo.MessageSend{
					Reference: msg.Reference(),
					Content:   fmt.Sprintf("%s woke up after %s", username, (time.Since(sleepDate)/time.Second)*time.Second),
				}

				content := []string{}
				for _, v := range sMembersCmd.Val() {
					splits := strings.SplitN(v, " ", 3)
					st, err := s.ChannelMessage(splits[0], splits[1])
					if err == nil {
						at, _ := st.Timestamp.Parse()
						content = append(content, fmt.Sprintf("%s mentioned you %s ago: [here](https://discord.com/channels/%s/%s/%s)", st.Author.Mention(), (time.Since(at)/time.Second)*time.Second, splits[2], st.ChannelID, st.ID))
						if len(content) == 20 {
							break
						}
					}
				}
				if len(content) != 0 {
					data.Embed = &discordgo.MessageEmbed{
						Color: s.State.MessageColor(msg.Message),
						Author: &discordgo.MessageEmbedAuthor{
							Name:    fmt.Sprintf("People who mentioned %s", msg.Author.Username),
							IconURL: msg.Author.AvatarURL(""),
						},
						Description: strings.Join(content, "\n"),
					}
				}

				_, err := s.ChannelMessageSendComplex(msg.ChannelID, data)
				if err != nil {
					logrus.Error("failed to send message: ", err)
				}

				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("%s has gone to sleep somebody tuck them!", username), msg.Reference())
			return err
		},
	}
}

func (m *Module) TuckCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "tuck"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "tuck")
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			guild, err := s.State.Guild(msg.GuildID)
			if err != nil {
				return err
			}

			search := strings.TrimSpace(strings.ToLower(strings.Join(path, " ")))
			member := utils.FindMember(s, guild, msg.Message, search)

			if member == nil && search == "" {
				msg.Member.User = msg.Author
				member = msg.Member
			}

			set, err := m.gCtx.Inst().Redis.Exists(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
			if err != nil {
				return err
			}

			if set && (member == nil || member.User.ID != msg.Author.ID) {
				pipe := m.gCtx.Inst().Redis.Pipeline(m.gCtx)
				getCmd := pipe.Get(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
				sMembersCmd := pipe.SMembers(m.gCtx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s:tucked", msg.Author.ID))
				pipe.Del(m.gCtx, fmt.Sprintf("sleepers:%s:mentions", msg.Author.ID))
				_, _ = pipe.Exec(m.gCtx)

				val, _ := getCmd.Result()
				sleepDate, _ := time.Parse(time.RFC3339, val)
				data := &discordgo.MessageSend{
					Reference: msg.Reference(),
					Content:   fmt.Sprintf("%s woke up after %s", msg.Author, (time.Since(sleepDate)/time.Second)*time.Second),
				}

				content := []string{}
				for _, v := range sMembersCmd.Val() {
					splits := strings.SplitN(v, " ", 3)
					st, err := s.ChannelMessage(splits[0], splits[1])
					if err == nil {
						at, _ := st.Timestamp.Parse()
						content = append(content, fmt.Sprintf("%s mentioned you %s ago: [here](https://discord.com/channels/%s/%s/%s)", st.Author.Mention(), (time.Since(at)/time.Second)*time.Second, splits[2], st.ChannelID, st.ID))
						if len(content) == 20 {
							break
						}
					}
				}
				if len(content) != 0 {
					data.Embed = &discordgo.MessageEmbed{
						Color: s.State.MessageColor(msg.Message),
						Author: &discordgo.MessageEmbedAuthor{
							Name:    fmt.Sprintf("People who mentioned %s", msg.Author.Username),
							IconURL: msg.Author.AvatarURL(""),
						},
						Description: strings.Join(content, "\n"),
					}
				}

				_, err := s.ChannelMessageSendComplex(msg.ChannelID, data)
				if err != nil {
					logrus.Error("failed to send message: ", err)
				}

				return err
			}

			if member == nil {
				if search == "" {
					st, err := s.ChannelMessageSendReply(msg.ChannelID, "Invalid usage: `!tuck <user ...> <message>`", msg.Reference())
					utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
					return err
				}

				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if exists, _ := m.gCtx.Inst().Redis.Exists(m.gCtx, fmt.Sprintf("sleepers:%s", member.User.ID)); !exists {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("%s isnt even sleeping.", member.User), msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			set, err = m.gCtx.Inst().Redis.SetNX(m.gCtx, fmt.Sprintf("sleepers:%s:tucked", member.User.ID), "", 0)
			if err != nil {
				return err
			}

			if set {
				_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("%s has tucked %s to bed.", msg.Author.Mention(), member.User), msg.Reference())
				return err
			}

			st, err := s.ChannelMessageSendReply(msg.ChannelID, "Tucking the tucked WeirdChamp", msg.Reference())
			utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
			return err
		},
	}
}
