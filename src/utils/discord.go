package utils

import (
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

func CleanUpMessageDeny(s *discordgo.Session, msg *discordgo.Message, ttl time.Duration, id string) {
	if msg != nil {
		go func() {
			time.Sleep(ttl)
			if id != "" {
				if err := s.MessageReactionAdd(msg.ChannelID, id, "❌"); err == nil {
					_ = s.ChannelMessageDelete(msg.ChannelID, msg.ID)
				} else {
					logrus.Error("failed to react: ", err)
				}
			} else {
				_ = s.ChannelMessageDelete(msg.ChannelID, msg.ID)
			}
		}()
	}
}

func CleanUpMessageAccept(s *discordgo.Session, msg *discordgo.Message, ttl time.Duration, id string) {
	if msg != nil {
		go func() {
			time.Sleep(ttl)
			if id != "" {
				if err := s.MessageReactionAdd(msg.ChannelID, id, "✅"); err == nil {
					_ = s.ChannelMessageDelete(msg.ChannelID, msg.ID)
				} else {
					logrus.Error("failed to react: ", err)
				}
			} else {
				_ = s.ChannelMessageDelete(msg.ChannelID, msg.ID)
			}
		}()
	}
}

func FindMember(s *discordgo.Session, guild *discordgo.Guild, msg *discordgo.Message, search string) *discordgo.Member {
	var member *discordgo.Member
	if len(msg.Mentions) != 0 {
		member, err := s.State.Member(msg.GuildID, msg.Mentions[0].ID)
		if err != nil {
			return nil
		}
		return member
	}

	if search != "" {
		return FindMemberSearch(guild, search)
	}

	return member
}

func FindMemberSearch(guild *discordgo.Guild, search string) *discordgo.Member {
	nameLength := -1
	var member *discordgo.Member

	for _, v := range guild.Members {
		if v.User.ID == search {
			return v
		}

		fullNick := strings.ToLower(v.Nick) + "#" + v.User.Discriminator
		fullName := strings.ToLower(v.User.Username) + "#" + v.User.Discriminator

		if v.Nick != "" && strings.Contains(fullNick, search) {
			if len(fullNick) < nameLength || nameLength == -1 {
				nameLength = len(fullNick)
				member = v
				if nameLength == len(search) {
					return v
				}
			}
		}
		if strings.Contains(fullName, search) {
			if len(fullName) < nameLength || nameLength == -1 {
				nameLength = len(fullName)
				member = v
				if nameLength == len(search) {
					return v
				}
			}
		}
	}
	return member
}
