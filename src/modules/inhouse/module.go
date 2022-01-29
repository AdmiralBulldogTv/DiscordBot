package inhouse

import (
	"fmt"
	"strings"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/bwmarrin/discordgo"
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

	err := multierror.Append(nil, gCtx.Inst().Discord.RegisterCommand("inhouse", m.CommandGroup()))
	closeFns = append(closeFns, gCtx.Inst().Discord.AddHandler(m.onMessage))

	go func() {
		<-gCtx.Done()
		for _, fn := range closeFns {
			fn()
		}
		close(m.done)
	}()

	return m.done, err.ErrorOrNil()
}

func (m *Module) Name() string {
	return "Points"
}

func (m *Module) onMessage(s *discordgo.Session, msg *discordgo.MessageCreate) {
	if msg.GuildID != m.gCtx.Config().Discord.GuildID || msg.Author.Bot {
		return
	}

	mp := map[string]bool{}
	for _, r := range msg.Member.Roles {
		mp[r] = true
	}

	if !mp[m.gCtx.Config().Modules.InHouse.InhouseRoleID] {
		return
	}

	for _, role := range m.gCtx.Config().Modules.InHouse.RequiredRoleIDs {
		if mp[role] {
			return
		}
	}

	if err := s.GuildMemberRoleRemove(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.InhouseRoleID); err != nil {
		logrus.Errorf("cannot remove role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
	}
}

func (m *Module) CommandGroup() command.Cmd {
	return &command.CommandGroup{
		NameCmd: func() string {
			return "points"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "inhouse")
		},
		Commands: map[string]command.Cmd{
			"join":      m.JoinCmd(),
			"leave":     m.LeaveCmd(),
			"gold":      m.GoldCmd(),
			"take-gold": m.TakeGoldCmd(),
			"add":       m.AddCmd(),
			"remove":    m.RemoveCmd(),
			"ping":      m.PingCmd(),
		},
	}
}

func (m *Module) JoinCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "join")
		},
		NameCmd: func() string {
			return "inhouse join"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			mp := map[string]bool{}
			for _, r := range msg.Member.Roles {
				mp[r] = true
			}

			found := false
			for _, role := range m.gCtx.Config().Modules.InHouse.RequiredRoleIDs {
				if mp[role] {
					found = true
				}
			}

			if !found {
				return nil
			}

			if mp[m.gCtx.Config().Modules.InHouse.InhouseRoleID] {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "You are already in the inhouse league", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleAdd(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.InhouseRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err := s.ChannelMessageSendReply(msg.ChannelID, "Welcome to the inhouse league!", msg.Reference())
			return err
		},
	}
}

func (m *Module) LeaveCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "leave")
		},
		NameCmd: func() string {
			return "inhouse leave"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			mp := map[string]bool{}
			for _, r := range msg.Member.Roles {
				mp[r] = true
			}

			found := false
			for _, role := range m.gCtx.Config().Modules.InHouse.RequiredRoleIDs {
				if mp[role] {
					found = true
				}
			}

			if !found {
				return nil
			}

			if !mp[m.gCtx.Config().Modules.InHouse.InhouseRoleID] {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "You are not in the inhouse league", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleRemove(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.InhouseRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err := s.ChannelMessageSendReply(msg.ChannelID, "You have left the inhouse league", msg.Reference())
			return err
		},
	}
}

func (m *Module) GoldCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "gold")
		},
		NameCmd: func() string {
			return "inhouse gold"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			perms, err := s.State.MessagePermissions(msg.Message)
			if err != nil {
				return err
			}

			if perms&discordgo.PermissionAdministrator == 0 {
				mp := map[string]bool{}
				for _, v := range msg.Member.Roles {
					mp[v] = true
				}
				found := false
				for _, v := range m.gCtx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			guild, err := s.State.Guild(msg.GuildID)
			if err != nil {
				return err
			}

			search := strings.TrimSpace(strings.ToLower(strings.Join(path, " ")))
			member := utils.FindMember(s, guild, msg.Message, search)
			if member == nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleAdd(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.GoldRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("You gave gold to %s", member.User), msg.Reference())
			return err
		},
	}
}

func (m *Module) AddCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "add")
		},
		NameCmd: func() string {
			return "inhouse add"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			perms, err := s.State.MessagePermissions(msg.Message)
			if err != nil {
				return err
			}

			if perms&discordgo.PermissionAdministrator == 0 {
				mp := map[string]bool{}
				for _, v := range msg.Member.Roles {
					mp[v] = true
				}
				found := false
				for _, v := range m.gCtx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			guild, err := s.State.Guild(msg.GuildID)
			if err != nil {
				return err
			}

			search := strings.TrimSpace(strings.ToLower(strings.Join(path, " ")))
			member := utils.FindMember(s, guild, msg.Message, search)
			if member == nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleAdd(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.InhouseRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("You added %s to the inhouse league", member.User), msg.Reference())
			return err
		},
	}
}

func (m *Module) RemoveCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "remove")
		},
		NameCmd: func() string {
			return "inhouse remove"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			perms, err := s.State.MessagePermissions(msg.Message)
			if err != nil {
				return err
			}

			if perms&discordgo.PermissionAdministrator == 0 {
				mp := map[string]bool{}
				for _, v := range msg.Member.Roles {
					mp[v] = true
				}
				found := false
				for _, v := range m.gCtx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			guild, err := s.State.Guild(msg.GuildID)
			if err != nil {
				return err
			}

			search := strings.TrimSpace(strings.ToLower(strings.Join(path, " ")))
			member := utils.FindMember(s, guild, msg.Message, search)
			if member == nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleRemove(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.InhouseRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("You removed %s from the inhouse league", member.User), msg.Reference())
			return err
		},
	}
}

func (m *Module) TakeGoldCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "take-gold")
		},
		NameCmd: func() string {
			return "inhouse take-gold"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			perms, err := s.State.MessagePermissions(msg.Message)
			if err != nil {
				return err
			}

			if perms&discordgo.PermissionAdministrator == 0 {
				mp := map[string]bool{}
				for _, v := range msg.Member.Roles {
					mp[v] = true
				}
				found := false
				for _, v := range m.gCtx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			guild, err := s.State.Guild(msg.GuildID)
			if err != nil {
				return err
			}

			search := strings.TrimSpace(strings.ToLower(strings.Join(path, " ")))
			member := utils.FindMember(s, guild, msg.Message, search)
			if member == nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			if err := s.GuildMemberRoleRemove(m.gCtx.Config().Discord.GuildID, msg.Author.ID, m.gCtx.Config().Modules.InHouse.GoldRoleID); err != nil {
				logrus.Errorf("cannot add role (%s) from user (%s): %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author.ID, err.Error())
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("You removed gold from %s", member.User), msg.Reference())
			return err
		},
	}
}

func (m *Module) PingCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "ping")
		},
		NameCmd: func() string {
			return "inhouse ping"
		},
		ExecuteCmd: func(s *discordgo.Session, msg *discordgo.MessageCreate, path []string) error {
			perms, err := s.State.MessagePermissions(msg.Message)
			if err != nil {
				return err
			}

			if perms&discordgo.PermissionAdministrator == 0 {
				mp := map[string]bool{}
				for _, v := range msg.Member.Roles {
					mp[v] = true
				}
				found := false
				for _, v := range m.gCtx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			_, err = s.ChannelMessageSend(msg.ChannelID, fmt.Sprintf("<&!%s> pingged by %s", m.gCtx.Config().Modules.InHouse.InhouseRoleID, msg.Author))
			return err
		},
	}
}
