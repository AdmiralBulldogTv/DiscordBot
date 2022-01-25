package common

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
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

	err := multierror.Append(nil, gCtx.Inst().Discord.RegisterCommand("avatar", m.AvatarCmd()))
	err = multierror.Append(err, gCtx.Inst().Discord.RegisterCommand("dank", m.DankCmd()))
	err = multierror.Append(err, gCtx.Inst().Discord.RegisterCommand("based", m.BasedCmd()))

	go func() {
		<-gCtx.Done()
		close(m.done)
	}()

	return m.done, err
}

func (m *Module) Name() string {
	return "Common"
}

func (m *Module) AvatarCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "avatar"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "avatar")
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

			if member == nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			_, err = s.ChannelMessageSendComplex(msg.ChannelID, &discordgo.MessageSend{
				Reference: msg.Reference(),
				Embed: &discordgo.MessageEmbed{
					Title: fmt.Sprintf("%s's Avatar", member.User),
					Color: s.State.UserColor(member.User.ID, msg.ChannelID),
					Image: &discordgo.MessageEmbedImage{
						URL:    member.User.AvatarURL("1024"),
						Width:  1024,
						Height: 1024,
					},
				},
			})

			return err
		},
	}
}

func (m *Module) DankCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "dank"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "dank")
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			found := false
			for _, v := range msg.Member.Roles {
				if v == m.gCtx.Config().Modules.Common.DankRoleID {
					found = true
					break
				}
			}
			if !found {
				return nil
			}

			set, err := m.gCtx.Inst().Redis.SetNX(m.gCtx, "dank-global-cooldown", "1", time.Minute*20)
			if err != nil {
				return err
			}
			if !set {
				ttl, err := m.gCtx.Inst().Redis.TTL(m.gCtx, "dank-global-cooldown")
				if err != nil {
					return err
				}
				st, err := s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Command is on cooldown, try again in %s", (ttl/time.Second)*time.Second), msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			var color int
			for i := 0; i < 3; i++ {
				color |= rand.Intn(255) << (i * 8)
			}

			_, err = s.GuildRoleEdit(msg.GuildID, m.gCtx.Config().Modules.Common.DankRoleID, "Dank Memers", color, false, 0, false)
			if err != nil {
				return err
			}
			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Changed the color of dank memers to #%06x", color), msg.Reference())
			return err
		},
	}
}

func (m *Module) BasedCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "based"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "based")
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			found := false
			for _, v := range msg.Member.Roles {
				if v == m.gCtx.Config().Modules.Common.BasedRoleID {
					found = true
					break
				}
			}
			if !found || len(m.gCtx.Config().Modules.Common.BasedRoleColors) == 0 {
				return nil
			}

			set, err := m.gCtx.Inst().Redis.SetNX(m.gCtx, "based-global-cooldown", "1", time.Minute*5)
			if err != nil {
				return err
			}
			if !set {
				ttl, err := m.gCtx.Inst().Redis.TTL(m.gCtx, "based-global-cooldown")
				if err != nil {
					return err
				}
				st, err := s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Command is on cooldown, try again in %s", (ttl/time.Second)*time.Second), msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			color := m.gCtx.Config().Modules.Common.BasedRoleColors[rand.Intn(len(m.gCtx.Config().Modules.Common.BasedRoleColors))]

			_, err = s.GuildRoleEdit(msg.GuildID, m.gCtx.Config().Modules.Common.BasedRoleID, "Based Memers", color, true, 0, false)
			if err != nil {
				return err
			}
			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Changed the color of dank memers to #%06x", color), msg.Reference())
			return err
		},
	}
}
