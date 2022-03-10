package steam

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/Philipp15b/go-steam/v3"
	"github.com/Philipp15b/go-steam/v3/protocol/protobuf/unified"
	"github.com/Philipp15b/go-steam/v3/protocol/steamlang"
	"github.com/Philipp15b/go-steam/v3/socialcache"
	"github.com/Philipp15b/go-steam/v3/steamid"
	"github.com/Philipp15b/go-steam/v3/totp"
	"github.com/davecgh/go-spew/spew"

	"github.com/sirupsen/logrus"
)

type Client struct {
	Config *Config

	client *steam.Client
	done   chan struct{}
}

type Config struct {
	Details              AccDetails
	OnRelationshipChange func(steamlang.EFriendRelationship, steamid.SteamId)
	OnNicknameChange     func(steamid.SteamId, string, string)
	OnLogin              func()
	OnFriendsLoad        func()
	OnNicknamesLoad      func()
	OnDisconnect         func()
	OnRawEvent           func(interface{})
}

type AccDetails struct {
	TotpSecret string
	Username   string
	Password   string
}

func NewClient(ctx context.Context, config *Config) *Client {
	localLog := logrus.WithField("username", config.Details.Username)

	client := &Client{
		Config: config,
		client: steam.NewClient(),
		done:   make(chan struct{}),
	}
	once := sync.Once{}

	lastReconnect := time.Time{}
	reconnectIssues := 0

	go func() {
		ch := client.client.Events()
		for event := range ch {
			if config.OnRawEvent != nil {
				config.OnRawEvent(event)
			}

			switch ev := event.(type) {
			case *steam.ConnectedEvent:
				var (
					code string
					err  error
				)
				if config.Details.TotpSecret != "" {
					code, err = totp.GenerateTotpCode(config.Details.TotpSecret, time.Now())
					if err != nil {
						localLog.WithError(err).Fatal("steam totp")
					}
					logrus.Info("generated steam totp for " + config.Details.Username)
				}

				client.client.Auth.LogOn(&steam.LogOnDetails{
					Username:      config.Details.Username,
					Password:      config.Details.Password,
					TwoFactorCode: code,
				})
			case *steam.DisconnectedEvent:
				if config.OnDisconnect != nil {
					config.OnDisconnect()
				}

				if ctx.Err() != nil {
					once.Do(func() {
						close(client.done)
					})
				}
			case *steam.MachineAuthUpdateEvent:
			case *steam.LogOnFailedEvent:
				localLog.WithError(fmt.Errorf(spew.Sdump(event))).Fatalf("steam %s login failed", config.Details.Username)
			case *steam.LoggedOnEvent:
				localLog.Infof("steam %s connected", config.Details.Username)
				if time.Since(lastReconnect) < 30*time.Second {
					reconnectIssues++
				} else {
					reconnectIssues = 0
				}

				if reconnectIssues > 5 {
					localLog.Fatal("reconnect isues detected restarting bot")
				}

				if config.OnLogin != nil {
					config.OnLogin()
				}
			case *steam.FriendsListEvent:
				localLog.Infof("steam %s friend list recieved", config.Details.Username)
				if config.OnFriendsLoad != nil {
					config.OnFriendsLoad()
				}
			case *steam.FriendStateEvent:
				if config.OnRelationshipChange != nil {
					config.OnRelationshipChange(ev.Relationship, ev.SteamId)
				}
			case *steam.NicknameListEvent:
				localLog.Infoln("nicknames recieved from steam")
				if config.OnNicknamesLoad != nil {
					config.OnNicknamesLoad()
				}
			case *steam.UnhandledPacketEvent:
				if ev.Packet.EMsg == steamlang.EMsg_ServiceMethod && bytes.Contains(ev.Packet.Data, utils.S2B("NotifyFriendNicknameChanged")) {
					event := new(unified.CPlayer_FriendNicknameChanged_Notification)
					ev.Packet.ReadProtoMsg(event)
					var old string
					if event.GetNickname() == "" {
						client.client.Social.Nicknames.Remove(utils.SteamID3ToSteamID(uint64(event.GetAccountid())))
					} else {
						sid := utils.SteamID3ToSteamID(uint64(event.GetAccountid()))
						if v, ok := client.client.Social.Nicknames.GetCopy()[sid]; !ok {
							client.client.Social.Nicknames.Add(socialcache.Nickname{
								SteamId:  utils.SteamID3ToSteamID(uint64(event.GetAccountid())),
								Nickname: event.GetNickname(),
							})
						} else {
							old = v.Nickname
							client.client.Social.Nicknames.SetName(utils.SteamID3ToSteamID(uint64(event.GetAccountid())), event.GetNickname())
						}
					}
					if config.OnNicknameChange != nil {
						config.OnNicknameChange(utils.SteamID3ToSteamID(uint64(event.GetAccountid())), old, event.GetNickname())
					}
				}
			}
		}
	}()

	_, err := client.client.Connect()
	if err != nil {
		logrus.WithError(err).Fatal("steam")
	}

	go func() {
		tick := time.NewTicker(time.Second * 10)
		failedCount := 0
		for {
			select {
			case <-ctx.Done():
			case <-tick.C:
				if !client.client.Connected() {
					if _, err := client.client.Connect(); err != nil {
						localLog.WithError(err).Error("steam connection")
						failedCount++
					}
					if failedCount > 10 {
						localLog.Fatal("steam failed to connect after 10 attempts")
					}
				}
			}
		}
	}()

	go func() {
		<-ctx.Done()
		if client.client.Connected() {
			client.client.Disconnect()
		} else {
			once.Do(func() {
				close(client.done)
			})
		}
	}()

	return client
}

func (c *Client) RenameFriend(id steamid.SteamId, nickname string) {
	c.client.Social.NicknameFriend(id, nickname)
}

func (c *Client) AddFriend(id steamid.SteamId) {
	c.client.Social.AddFriend(id)
}

func (c *Client) RemoveFriend(id steamid.SteamId) {
	c.client.Social.RemoveFriend(id)
}

func (c *Client) Friends() map[steamid.SteamId]socialcache.Friend {
	return c.client.Social.Friends.GetCopy()
}

func (c *Client) NicknameByID(id steamid.SteamId) string {
	if nickname, err := c.client.Social.Nicknames.ById(id); err == nil {
		return nickname.Nickname
	}
	return ""
}

func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) Raw() *steam.Client {
	return c.client
}
