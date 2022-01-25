package points

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/structures"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/mongo"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
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

	err := multierror.Append(nil, gCtx.Inst().Discord.RegisterCommand("points", m.PointsCmd()))
	err = multierror.Append(err, gCtx.Inst().Discord.RegisterCommand("add-points", m.AddPointsCmd()))
	err = multierror.Append(err, gCtx.Inst().Discord.RegisterCommand("set-points", m.SetPointsCmd()))
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
	return "Points"
}

func (m *Module) onMessage(s *discordgo.Session, msg *discordgo.MessageCreate) {
	if msg.GuildID != m.gCtx.Config().Discord.GuildID || msg.Author.Bot {
		return
	}

	userID := msg.Author.ID
	ctx, cancel := context.WithTimeout(m.gCtx, time.Second*5)
	defer cancel()

	failurePipe := m.gCtx.Inst().Redis.Pipeline(ctx)
	runFailurePipe := true

	defer func() {
		if runFailurePipe {
			_, _ = failurePipe.Exec(m.gCtx)
		}
	}()

	{
		pipe := m.gCtx.Inst().Redis.Pipeline(ctx)
		hourlyKey := fmt.Sprintf("message-limits-hourly:%s", userID)
		hourlyIncrCmd := pipe.IncrBy(ctx, hourlyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		failurePipe.DecrBy(m.gCtx, hourlyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		hourlyTTLCmd := pipe.TTL(ctx, hourlyKey)
		_, err := pipe.Exec(ctx)
		if err != nil {
			logrus.Error("failed to add to daily limit: ", err)
			return
		}

		ttl := hourlyTTLCmd.Val()
		if ttl == -1 {
			if err = m.gCtx.Inst().Redis.Expire(ctx, hourlyKey, time.Hour); err != nil {
				logrus.Error("failed to expire key: ", err)
				return
			}
		}

		hourlyValue := hourlyIncrCmd.Val()
		if hourlyValue > int64(m.gCtx.Config().Modules.Points.HourlyLimit) {
			// we dont have to check further since they exceeded the hourly limit
			return
		}
	}

	{
		pipe := m.gCtx.Inst().Redis.Pipeline(ctx)
		dailyKey := fmt.Sprintf("message-limits-daily:%s", userID)
		dailyIncrCmd := pipe.IncrBy(ctx, dailyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		failurePipe.DecrBy(m.gCtx, dailyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		dailyTTLCmd := pipe.TTL(ctx, dailyKey)
		_, err := pipe.Exec(ctx)
		if err != nil {
			logrus.Error("failed to add to daily limit: ", err)
			return
		}

		ttl := dailyTTLCmd.Val()
		if ttl == -1 {
			if err = m.gCtx.Inst().Redis.Expire(ctx, dailyKey, time.Hour*24); err != nil {
				logrus.Error("failed to expire key: ", err)
				return
			}
		}

		dailyValue := dailyIncrCmd.Val()
		if dailyValue > int64(m.gCtx.Config().Modules.Points.DailyLimit) {
			// we dont have to check further since they exceeded the daily limit
			return
		}
	}

	{
		pipe := m.gCtx.Inst().Redis.Pipeline(ctx)
		weeklyKey := fmt.Sprintf("message-limits-weekly:%s", userID)
		weeklyIncrCmd := pipe.IncrBy(ctx, weeklyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		failurePipe.DecrBy(m.gCtx, weeklyKey, int64(m.gCtx.Config().Modules.Points.PointsPerMessage))
		weeklyTTLCmd := pipe.TTL(ctx, weeklyKey)
		_, err := pipe.Exec(ctx)
		if err != nil {
			logrus.Error("failed to add to daily limit: ", err)
			return
		}

		ttl := weeklyTTLCmd.Val()
		if ttl == -1 {
			if err = m.gCtx.Inst().Redis.Expire(ctx, weeklyKey, time.Hour*24*7); err != nil {
				logrus.Error("failed to expire key: ", err)
				return
			}
		}

		weeklyValue := weeklyIncrCmd.Val()
		if weeklyValue > int64(m.gCtx.Config().Modules.Points.WeeklyLimit) {
			// we dont have to check further since they exceeded the weekly limit
			return
		}
	}

	opts := options.FindOneAndUpdate().SetUpsert(true)

	user := structures.User{}

	// at this point we know they can get more points
	res := m.gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOneAndUpdate(ctx, bson.M{
		"discord.id": msg.Author.ID,
	}, bson.M{
		"$set": bson.M{
			"discord": structures.UserDiscord{
				ID:            msg.Author.ID,
				Name:          msg.Author.Username,
				Discriminator: msg.Author.Discriminator,
			},
		},
		"$inc": bson.M{
			"modules.points.points": int32(m.gCtx.Config().Modules.Points.PointsPerMessage),
		},
	}, opts)
	err := res.Err()
	if err == nil {
		err = res.Decode(&user)
	}
	if err != nil && err != mongo.ErrNoDocuments {
		logrus.Error("failed to update user: ", err)
	} else {
		runFailurePipe = false
	}

	if msg.Member == nil {
		msg.Member, err = s.GuildMember(m.gCtx.Config().Discord.GuildID, msg.Author.ID)
		if err != nil {
			logrus.Errorf("failed to fetch member (%s#%s - %s): %s", msg.Author.Username, msg.Author.Discriminator, msg.Author.ID, err.Error())
			return
		}
	}

	mp := map[string]bool{}
	for _, v := range msg.Member.Roles {
		mp[v] = true
	}

	hasRequiredRole := m.gCtx.Config().Modules.Points.RequiredRoleID == "" || mp[m.gCtx.Config().Modules.Points.RequiredRoleID]
	for _, role := range m.gCtx.Config().Modules.Points.Roles {
		if hasRequiredRole && role.Points <= int(user.Modules.Points.Points)+10 && !mp[role.ID] {
			if err := s.GuildMemberRoleAdd(m.gCtx.Config().Discord.GuildID, msg.Author.ID, role.ID); err != nil {
				logrus.Errorf("cannot add role (%s) to user (%s): %s", role.ID, msg.Author.ID, err.Error())
			}
		} else if !hasRequiredRole || (role.Points > int(user.Modules.Points.Points)+10 && mp[role.ID]) {
			if err := s.GuildMemberRoleRemove(m.gCtx.Config().Discord.GuildID, msg.Author.ID, role.ID); err != nil {
				logrus.Errorf("cannot remove role (%s) from user (%s): %s", role.ID, msg.Author.ID, err.Error())
			}
		}
	}
}

func (m *Module) PointsCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "points"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "points")
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

			user := structures.User{}
			res := m.gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(m.gCtx, bson.M{
				"discord.id": member.User.ID,
			})
			err = res.Err()
			if err == nil {
				err = res.Decode(&user)
			}

			if err != nil {
				if err == mongo.ErrNoDocuments {
					_, err := s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("%s has 0 points.", member.User), msg.Reference())
					return err
				}

				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("%s has %d points.", member.User, user.Modules.Points.Points), msg.Reference())
			return err
		},
	}
}

func (m *Module) AddPointsCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "add-points"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "add-points")
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

			if len(path) < 2 {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Invalid usage: `!add-points <user ...> <points>`\nExample: `!add-points Troy 100`", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			value, err := strconv.Atoi(path[len(path)-1])
			if err != nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Invalid usage: `!add-points <user ...> <points>`\nExample: `!add-points Troy 100`", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			path = path[:len(path)-1]

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

			opts := options.Update().SetUpsert(true)
			_, err = m.gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).UpdateOne(m.gCtx, bson.M{
				"discord.id": member.User.ID,
			}, bson.M{
				"$set": bson.M{
					"discord": structures.UserDiscord{
						ID:            member.User.ID,
						Name:          member.User.Username,
						Discriminator: member.User.Discriminator,
					},
				},
				"$inc": bson.M{
					"modules.points.points": int32(value),
				},
			}, opts)

			if err != nil {
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Added %d points to %s.", value, member.User.Username), msg.Reference())
			return err
		},
	}
}

func (m *Module) SetPointsCmd() command.Cmd {
	return &command.Command{
		NameCmd: func() string {
			return "set-points"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "set-points")
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

			if len(path) < 2 {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Invalid usage: `!set-points <user ...> <points>`\nExample: `!set-points Troy 100`", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			value, err := strconv.Atoi(path[len(path)-1])
			if err != nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Invalid usage: `!set-points <user ...> <points>`\nExample: `!set-points Troy 100`", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			path = path[:len(path)-1]

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

			opts := options.Update().SetUpsert(true)
			_, err = m.gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).UpdateOne(m.gCtx, bson.M{
				"discord.id": member.User.ID,
			}, bson.M{
				"$set": bson.M{
					"discord": structures.UserDiscord{
						ID:            member.User.ID,
						Name:          member.User.Username,
						Discriminator: member.User.Discriminator,
					},
					"modules.points.points": int32(value),
				},
			}, opts)

			if err != nil {
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, fmt.Sprintf("Set %s points to %d.", member.User.Username, value), msg.Reference())
			return err
		},
	}
}
