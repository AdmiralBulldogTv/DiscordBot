package tracker

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/structures"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/discord/command"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/dota2"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/mongo"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/steam"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/Philipp15b/go-steam/v3/protocol/steamlang"
	"github.com/Philipp15b/go-steam/v3/steamid"
	"github.com/bwmarrin/discordgo"
	"github.com/fasthttp/router"
	"github.com/hashicorp/go-multierror"
	jsoniter "github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/sirupsen/logrus"
)

var (
	subsGamesRegex = regexp.MustCompile(`(\d+)MC([^-]+)-?(.*)`)
	subsMainRegex  = regexp.MustCompile(`MC([^-]+)-?(.*)`)

	specialGamesRegex = regexp.MustCompile(`([^-]+)-?(.*)`)
	specialMainRegex  = regexp.MustCompile(`([^-]+)-?(.*)`)
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

type DiscordOAuthResp struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type DiscordUserConnection struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Revoked      bool   `json:"revoked"`
	Verified     bool   `json:"verified"`
	FriendSync   bool   `json:"friend_sync"`
	ShowActivity bool   `json:"show_activity"`
	Visibility   int    `json:"visibility"`
}

type Module struct {
	Ctx        global.Context
	DotaClient *dota2.DotaClient
	Games      *steam.Client
	Main       *steam.Client

	mainFriends sync.Map
	gameFriends sync.Map

	gamesOnce sync.Once
	mainOnce  sync.Once
	wg        sync.WaitGroup
}

func New() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "Tracker"
}

type APIMatch struct {
	MatchID int64 `json:"match_id"`
}

func (m *Module) Register(gCtx global.Context) (<-chan struct{}, error) {
	done := make(chan struct{})

	m.Ctx = gCtx

	m.wg.Add(2)

	m.DotaClient = dota2.New(gCtx, steam.AccDetails{
		TotpSecret: gCtx.Config().Modules.Tracker.Steam.Dota.TotpSecret,
		Username:   gCtx.Config().Modules.Tracker.Steam.Dota.Username,
		Password:   gCtx.Config().Modules.Tracker.Steam.Dota.Password,
	})
	m.Games = steam.NewClient(gCtx, &steam.Config{
		Details: steam.AccDetails{
			TotpSecret: gCtx.Config().Modules.Tracker.Steam.Games.TotpSecret,
			Username:   gCtx.Config().Modules.Tracker.Steam.Games.Username,
			Password:   gCtx.Config().Modules.Tracker.Steam.Games.Password,
		},
		OnRelationshipChange: m.gameRelationships,
		OnNicknameChange: func(sid steamid.SteamId, oldName, newName string) {
			logrus.Infof("steam nickname changed for %s from '%s' to '%s'", sid.String(), oldName, newName)
		},
		OnLogin: func() {
			logrus.Info("steam games client logged in")
		},
		OnFriendsLoad: func() {
			logrus.Info("games friends loaded")
			m.gameFriends = sync.Map{}
			for k, v := range m.Games.Friends() {
				m.gameFriends.Store(k, v.Relationship)
			}
		},
		OnNicknamesLoad: m.gameNicknameLoad,
		OnDisconnect: func() {
			logrus.Info("steam games client disconnected")
		},
	})
	m.Main = steam.NewClient(gCtx, &steam.Config{
		Details: steam.AccDetails{
			TotpSecret: gCtx.Config().Modules.Tracker.Steam.Main.TotpSecret,
			Username:   gCtx.Config().Modules.Tracker.Steam.Main.Username,
			Password:   gCtx.Config().Modules.Tracker.Steam.Main.Password,
		},
		OnRelationshipChange: m.mainRelationships,
		OnNicknameChange: func(sid steamid.SteamId, oldName, newName string) {
			logrus.Infof("steam nickname changed for %s from '%s' to '%s'", sid.String(), oldName, newName)
		},
		OnLogin: func() {
			logrus.Info("steam main client logged in")
		},
		OnFriendsLoad: func() {
			logrus.Info("main friends loaded")
			m.mainFriends = sync.Map{}
			for k, v := range m.Main.Friends() {
				m.mainFriends.Store(k, v.Relationship)
			}
		},
		OnNicknamesLoad: m.mainNicknameLoad,
		OnDisconnect: func() {
			logrus.Info("steam main client disconnected")
		},
	})

	h := m.routes(gCtx)
	srv := fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			start := time.Now()
			defer func() {
				err := recover()
				if err != nil {
					ctx.Response.SetStatusCode(fasthttp.StatusInternalServerError)
				}
				log := logrus.WithFields(logrus.Fields{
					"path":     string(ctx.Path()),
					"status":   ctx.Response.StatusCode(),
					"duration": time.Since(start),
				})
				if err != nil {
					log.WithField("panic", err).Error()
				} else {
					log.Info("")
				}
			}()

			h(ctx)
		},
	}

	go func() {
		if err := srv.ListenAndServe(gCtx.Config().Modules.Tracker.HTTP.Bind); err != nil {
			logrus.Fatal("failed to listen http: ", err)
		}

		<-m.DotaClient.Done()
		<-m.Games.Done()
		<-m.Main.Done()

		close(done)
	}()

	go m.autoQueryStats()
	go func() {
		m.wg.Wait()
		m.autoAdjustNicknames()
	}()

	err := gCtx.Inst().Discord.RegisterCommand("dotagames-manage", m.CommandGroup())

	return done, err
}

func (m *Module) CommandGroup() command.Cmd {
	return &command.CommandGroup{
		NameCmd: func() string {
			return "dotagames-manage"
		},
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "dotagames-manage")
		},
		Commands: map[string]command.Cmd{
			"query":          m.QueryCmd(),
			"force-nickname": m.ForceNickname(),
		},
	}
}

func (m *Module) QueryCmd() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "query")
		},
		NameCmd: func() string {
			return "dotagames-manage query"
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
				for _, v := range m.Ctx.Config().Discord.AdminRoles {
					if mp[v] {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			matchIDs := []string{}
			for _, v := range path {
				if _, err := strconv.Atoi(v); err == nil {
					matchIDs = append(matchIDs, v)
				}
			}

			matches := m.queryMatchIDs(matchIDs)
			buf := bytes.Buffer{}
			buf.WriteString("Queried Matches:\n")
			for _, v := range matches {
				buf.WriteString(v.Game.GameID + "\n")
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, buf.String(), msg.Reference())
			return err
		},
	}
}

func (m *Module) ForceNickname() command.Cmd {
	return &command.Command{
		MatchCmd: func(path []string) bool {
			return len(path) != 0 && strings.EqualFold(path[0], "force-nickname")
		},
		NameCmd: func() string {
			return "dotagames-manage force-nickname"
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
				for _, v := range m.Ctx.Config().Discord.AdminRoles {
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

			res := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(context.Background(), bson.M{
				"discord.id": member.User.ID,
			})
			user := structures.User{}
			err = res.Err()
			if err == nil {
				err = res.Decode(&user)
			}
			if err != nil {
				if err == mongo.ErrNoDocuments {
					st, err := s.ChannelMessageSendReply(msg.ChannelID, "Couldn't find that user", msg.Reference())
					utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
					return err
				}

				return err
			}

			err = m.adjustNickname(context.Background(), user, sectionGames|sectionMain|forceNickname)
			if err != nil {
				st, err := s.ChannelMessageSendReply(msg.ChannelID, "Failed to adjust user's nickname", msg.Reference())
				utils.CleanUpMessageDeny(s, st, time.Second*10, msg.ID)
				return err
			}

			_, err = s.ChannelMessageSendReply(msg.ChannelID, "User's nickname has been adjusted", msg.Reference())
			return err
		},
	}
}

func (m *Module) WaitDotaReady(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		if m.DotaClient.Ready() {
			return ctx.Err()
		}
	}
}

func (m *Module) autoQueryStats() {
	first := true
	for {
		if !first {
			select {
			case <-time.After(time.Minute * 10):
			case <-m.Ctx.Done():
				return
			}
		} else {
			first = false
		}

		rdy, cancel := context.WithTimeout(m.Ctx, time.Minute)
		err := m.WaitDotaReady(rdy)
		cancel()
		if err != nil {
			logrus.Error("dota client not ready for stats query: ", err)
			continue
		}

		matchIDs := []string{}
		{
			logrus.Infoln("fetching matches")
			req, err := http.NewRequestWithContext(m.Ctx, "GET", fmt.Sprintf("https://api.opendota.com/api/players/%d/matches?project=match_id&limit=30", m.Games.Raw().SteamId().GetAccountId()), nil)
			if err != nil {
				logrus.WithError(err).Error("failed to get matches")
				continue
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				logrus.WithError(err).Error("failed to get matches")
				continue
			}

			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				body, err := ioutil.ReadAll(resp.Body)
				err = multierror.Append(err, fmt.Errorf("%s", body))
				logrus.WithError(err).Error("failed to get matches")
				continue
			}

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				logrus.WithError(err).Error("failed to get matches")
				continue
			}

			data := []APIMatch{}
			if err := json.Unmarshal(body, &data); err != nil {
				err = multierror.Append(err, fmt.Errorf("%s", body)).ErrorOrNil()
				logrus.WithError(err).Error("failed to get matches")
				continue
			}

			if len(data) == 0 {
				logrus.Debugln("no matches")
				continue
			}

			for _, v := range data {
				matchIDs = append(matchIDs, fmt.Sprint(v.MatchID))
			}
		}

		m.queryMatchIDs(matchIDs)
	}
}

func (m *Module) queryMatchIDs(matchIDs []string) []dota2.GameWrapper {
	{
		cur, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameDotaGames).Find(m.Ctx, bson.M{
			"game_id": bson.M{
				"$in": matchIDs,
			},
		})
		games := []structures.DotaGame{}
		if err == nil {
			err = cur.All(m.Ctx, &games)
		}
		if err != nil {
			logrus.Error("failed to query games: ", err)
			return nil
		}

		idMp := map[string]bool{}
		for _, v := range games {
			idMp[v.GameID] = true
		}

		newMatchIds := []string{}
		for _, v := range matchIDs {
			if !idMp[v] {
				newMatchIds = append(newMatchIds, v)
			}
		}
		matchIDs = newMatchIds
	}

	{
		games, err := m.DotaClient.QueryGames(m.Ctx, m.Ctx, m.Games.Raw().SteamId().GetAccountId(), matchIDs)
		if err != nil {
			logrus.Error("failed to query games: ", err)
			return nil
		}

		gamesDocs := []interface{}{}
		playerDocs := []interface{}{}
		if len(games) != 0 {
			playerIDs := []string{}
			for _, match := range games {
				for _, player := range match.Players {
					playerIDs = append(playerIDs, player.SteamID)
				}
			}

			users := []structures.User{}
			cur, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).Find(m.Ctx, bson.M{
				"steam.id": bson.M{
					"$in": playerIDs,
				},
			})
			if err == nil {
				err = cur.All(m.Ctx, &users)
			}
			if err != nil {
				logrus.Error("failed to query games: ", err)
				return nil
			}

			userMp := map[string]structures.User{}
			for _, user := range users {
				userMp[user.Steam.ID] = user
			}

			for i, match := range games {
				for j, player := range match.Players {
					player.UserID = userMp[player.SteamID].ID
					match.Players[j] = player
				}
				games[i] = match
			}

			for _, v := range games {
				gamesDocs = append(gamesDocs, v.Game)
				for _, v := range v.Players {
					playerDocs = append(playerDocs, v)
				}
			}

			totalErr := error(nil)
			{
				_, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameDotaGames).InsertMany(m.Ctx, gamesDocs)
				totalErr = multierror.Append(totalErr, err).ErrorOrNil()
			}

			{
				_, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameDotaGamePlayers).InsertMany(m.Ctx, playerDocs)
				totalErr = multierror.Append(totalErr, err).ErrorOrNil()
			}

			if totalErr != nil {
				logrus.Error("failed to insert documents: ", err)
			} else {
				for _, v := range userMp {
					if err := m.adjustNickname(m.Ctx, v, sectionGames); err != nil {
						logrus.Errorf("failed to adjust nickname for user: %s %s", v.Discord.ID, err.Error())
					}
				}

				logrus.Infof("Processed %d", len(games))
			}

		}

		return games
	}
}

func (m *Module) routes(gCtx global.Context) fasthttp.RequestHandler {
	handler := router.New()

	handler.GET("/", func(ctx *fasthttp.RequestCtx) {
		qs := url.Values{}
		qs.Add("client_id", gCtx.Config().Modules.Tracker.Discord.ClientID)
		qs.Add("redirect_uri", gCtx.Config().Modules.Tracker.Discord.RedirectURL)
		qs.Add("response_type", "code")
		qs.Add("scope", "identify connections")

		b, _ := utils.GenerateRandomBytes(32)
		state := hex.EncodeToString(b)

		qs.Add("state", state)

		cookie := &fasthttp.Cookie{}
		cookie.SetExpire(time.Now().Add(time.Minute * 5))
		cookie.SetKey("discord_csrf")
		cookie.SetHTTPOnly(true)
		cookie.SetSecure(gCtx.Config().Modules.Tracker.HTTP.CookieSecure)
		cookie.SetDomain(gCtx.Config().Modules.Tracker.HTTP.CookieDomain)
		cookie.SetValue(state)

		ctx.Response.Header.SetCookie(cookie)

		ctx.Redirect(fmt.Sprintf("https://discord.com/api/oauth2/authorize?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
	})

	handler.GET("/callback", func(ctx *fasthttp.RequestCtx) {
		state := ctx.QueryArgs().Peek("state")
		code := ctx.QueryArgs().Peek("code")
		qs := url.Values{}

		if utils.B2S(state) != utils.B2S(ctx.Request.Header.Cookie("discord_csrf")) {
			qs.Add("reason", "Invalid csrf cookie state")
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}

		if len(code) == 0 {
			qs.Add("reason", "Invalid response from discord")
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}

		qs.Add("client_id", gCtx.Config().Modules.Tracker.Discord.ClientID)
		qs.Add("client_secret", gCtx.Config().Modules.Tracker.Discord.ClientSecret)
		qs.Add("redirect_uri", gCtx.Config().Modules.Tracker.Discord.RedirectURL)
		qs.Add("grant_type", "authorization_code")
		qs.Add("code", utils.B2S(code))

		req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/v8/oauth2/token", bytes.NewBufferString(qs.Encode()))
		if err != nil {
			logrus.Error("failed to get oauth token: ", err)
			qs = url.Values{}
			qs.Add("reason", "Internal Server Error")
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}

		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logrus.Error("failed to get oauth token: ", err)
			qs = url.Values{}
			qs.Add("reason", "Internal Server Error")
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := ioutil.ReadAll(resp.Body)
			logrus.Errorf("failed to get oauth token: %s", body)
			qs = url.Values{}
			qs.Add("reason", fmt.Sprintf("Bad response from discord: %d", resp.StatusCode))
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}

		decoder := json.NewDecoder(resp.Body)
		discordResp := DiscordOAuthResp{}
		if err := decoder.Decode(&discordResp); err != nil {
			logrus.Errorf("failed to get oauth token: %s", err)
			qs = url.Values{}
			qs.Add("reason", "bad response from discord")
			ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
			return
		}

		{
			identify := false
			connections := false
			scopes := strings.Split(discordResp.Scope, " ")
			for _, v := range scopes {
				if v == "identify" {
					identify = true
				} else if v == "connections" {
					connections = true
				}
				if identify && connections {
					break
				}
			}

			if !connections || !identify {
				logrus.Debug("bad oauth scopes: ", discordResp.Scope)
				qs = url.Values{}
				qs.Add("reason", "bad response from discord")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
		}

		user := discordgo.User{}

		{
			req, _ := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/v9/users/@me", nil)
			req.Header.Add("Authorization", "Bearer "+discordResp.AccessToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				logrus.Errorf("failed to get user: %s", err)
				qs = url.Values{}
				qs.Add("reason", "bad response from discord")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}

			if resp.StatusCode != 200 {
				body, _ := ioutil.ReadAll(resp.Body)
				logrus.Errorf("failed to get user: %s", body)
				qs = url.Values{}
				qs.Add("reason", fmt.Sprintf("Bad response from discord: %d", resp.StatusCode))
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}

			decoder = json.NewDecoder(resp.Body)
			if err := decoder.Decode(&user); err != nil {
				logrus.Errorf("failed to get user: %s", err)
				qs = url.Values{}
				qs.Add("reason", "bad response from discord")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
		}

		connections := []DiscordUserConnection{}

		{
			req, _ := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/v8/users/@me/connections", nil)
			req.Header.Add("Authorization", "Bearer "+discordResp.AccessToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				logrus.Errorf("failed to get user: %s", err)
				qs = url.Values{}
				qs.Add("reason", "bad response from discord")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}

			if resp.StatusCode != 200 {
				body, _ := ioutil.ReadAll(resp.Body)
				logrus.Errorf("failed to get connections: %s", body)
				qs = url.Values{}
				qs.Add("reason", fmt.Sprintf("Bad response from discord: %d", resp.StatusCode))
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}

			decoder = json.NewDecoder(resp.Body)
			if err := decoder.Decode(&connections); err != nil {
				logrus.Errorf("failed to get connections: %s", err)
				qs = url.Values{}
				qs.Add("reason", "bad response from discord")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
		}

		steamConnection := DiscordUserConnection{}
		twitchConnection := DiscordUserConnection{}
		for _, v := range connections {
			if v.Visibility == 1 && v.Verified {
				if v.Type == "steam" && steamConnection.ID == "" {
					steamConnection = v
				} else if v.Type == "twitch" && twitchConnection.ID == "" {
					twitchConnection = v
				}
				if steamConnection.ID != "" && twitchConnection.ID != "" {
					break
				}
			}
		}

		steam := structures.UserSteam{
			ID:   steamConnection.ID,
			Name: steamConnection.Name,
		}

		twitch := structures.UserTwitch{
			ID:   twitchConnection.ID,
			Name: twitchConnection.Name,
		}

		discord := structures.UserDiscord{
			ID:            user.ID,
			Name:          user.Username,
			Discriminator: user.Discriminator,
		}

		{
			oldSteamUser := structures.User{}
			res := gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOneAndUpdate(ctx, bson.M{
				"steam.id": steam.ID,
			}, bson.M{
				"$set": bson.M{
					"steam": structures.UserSteam{},
				},
			})
			err := res.Err()
			if err == nil {
				err = res.Decode(&oldSteamUser)
			}
			if err != nil && err != mongo.ErrNoDocuments {
				logrus.Error("failed to unset steam mongo: ", err)
				qs = url.Values{}
				qs.Add("reason", "Internal Server Error")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
			if oldSteamUser.Discord.ID != "" && oldSteamUser.Discord.ID != discord.ID {
				if _, err := gCtx.Inst().Discord.SendPrivateMessage(user.ID, &discordgo.MessageSend{
					Content: "Your steam account has been unpaired.",
				}); err != nil {
					logrus.Error("failed to send message to user: ", err)
				}
			}
		}

		{
			oldTwitchUser := structures.User{}
			res := gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOneAndUpdate(ctx, bson.M{
				"twitch.id": steam.ID,
			}, bson.M{
				"$set": bson.M{
					"twitch": structures.UserTwitch{},
				},
			})
			err := res.Err()
			if err == nil {
				err = res.Decode(&oldTwitchUser)
			}
			if err != nil && err != mongo.ErrNoDocuments {
				logrus.Error("failed to unset steam mongo: ", err)
				qs = url.Values{}
				qs.Add("reason", "Internal Server Error")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
			if oldTwitchUser.Discord.ID != "" && oldTwitchUser.Discord.ID != discord.ID {
				if _, err := gCtx.Inst().Discord.SendPrivateMessage(user.ID, &discordgo.MessageSend{
					Content: "Your twitch account has been unpaired.",
				}); err != nil {
					logrus.Error("failed to send message to user: ", err)
				}
			}
		}

		uid := primitive.NewObjectIDFromTimestamp(time.Now())
		{
			res := gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOneAndUpdate(ctx, bson.M{
				"discord.id": user.ID,
			}, bson.M{
				"$set": bson.M{
					"discord": discord,
					"steam":   steam,
					"twitch":  twitch,
				},
				"$setOnInsert": bson.M{
					"_id": uid,
				},
			}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before))
			err := res.Err()
			oldUser := structures.User{}
			if err == nil {
				err = res.Decode(&oldUser)
			}
			if err != nil && err != mongo.ErrNoDocuments {
				logrus.Errorf("failed to insert object into database: %s", err)
				qs = url.Values{}
				qs.Add("reason", "Internal Server Error")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			} else {
				uid = oldUser.ID
			}
			if oldUser.Steam.ID != "" && oldUser.Steam.ID != steam.ID {
				id, _ := strconv.ParseUint(oldUser.Steam.ID, 10, 64)
				sid := utils.SteamID64ToSteamID(id)
				if _, ok := m.Games.Friends()[sid]; ok {
					m.Games.RemoveFriend(sid)
				}
				if _, ok := m.Main.Friends()[sid]; ok {
					m.Games.RemoveFriend(sid)
				}
			}
		}

		{
			_, err := gCtx.Inst().Mongo.Collection(mongo.CollectionNameDotaGamePlayers).UpdateMany(ctx, bson.M{
				"steam_id": steam.ID,
				"user_id":  primitive.NilObjectID,
			}, bson.M{
				"$set": bson.M{
					"user_id": uid,
				},
			})
			if err != nil {
				logrus.Errorf("failed to update previous player games object: %s", err)
				qs = url.Values{}
				qs.Add("reason", "Internal Server Error")
				ctx.Redirect(fmt.Sprintf("/failed?%s", qs.Encode()), fasthttp.StatusTemporaryRedirect)
				return
			}
		}

		content := []string{"Thank you for pairing."}

		if steam.ID == "" {
			content = append(content, "We could not find a valid steam account.")
		} else {
			content = append(content, fmt.Sprintf("We paired your steam account <https://steamcommunity.com/profiles/%s>", steam.ID))
		}

		if twitch.ID == "" {
			content = append(content, "We could not find a valid twitch account.")
		} else {
			content = append(content, fmt.Sprintf("We paired your twitch account <https://twitch.tv/%s>", twitch.Name), "If your twitch name is your old account this is not an issue, reconnect your twitch account to discord and repair.")
		}

		content = append(
			content,
			"If you have multiple twitch or steam accounts and the bot choose the wrong one toggle the visibility of the accounts you do not want paired off and toggle the visibility of the acounts you do want paird on and then re-pair using the link.",
			"You can see how to connect your accounts here https://i.nuuls.com/VxIr8.mp4",
		)

		if _, err := gCtx.Inst().Discord.SendPrivateMessage(user.ID, &discordgo.MessageSend{
			Content: strings.Join(content, "\n"),
		}); err != nil {
			logrus.Error("failed to send message to user: ", err)
		}

		{
			res := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(ctx, bson.M{"_id": uid})
			err := res.Err()
			user := structures.User{}
			if err == nil {
				err = res.Decode(&user)
			}
			if err != nil {
				logrus.Error("failed to adjust nickname of user: ", err)
			} else if err := m.adjustNickname(ctx, user, sectionGames|sectionMain); err != nil {
				logrus.Errorf("failed to adjust nickname for user: %s %s", user.Discord.ID, err.Error())
			}
		}

		ctx.Redirect("/paired", fasthttp.StatusTemporaryRedirect)
	})

	handler.GET("/paired", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyString("Accounts paired!")
	})

	handler.GET("/failed", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyString(fmt.Sprintf("Failed to pair accounts, please contact ales on discord.\nReason: %s", ctx.QueryArgs().Peek("reason")))
	})

	return handler.Handler
}

func (m *Module) gameRelationships(rel steamlang.EFriendRelationship, sid steamid.SteamId) {
	if v, ok := m.gameFriends.Load(sid); ok && v.(steamlang.EFriendRelationship) == rel {
		return
	}

	if rel == steamlang.EFriendRelationship_None {
		m.gameFriends.Delete(rel)
		return
	} else {
		m.gameFriends.Store(sid, rel)
	}

	logrus.Infof("GAMES: %s -> %s", sid, rel)

	id := utils.SteamIDToSteamID64(sid)
	res := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(context.Background(), bson.M{
		"steam.id": fmt.Sprint(id),
	})
	user := structures.User{}
	err := res.Err()
	if err == nil {
		err = res.Decode(&user)
	}
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return
		}

		logrus.Error("failed to fetch user from mongo: ", err)
		return
	}

	if err := m.adjustNickname(context.Background(), user, sectionGames); err != nil {
		logrus.Errorf("failed to adjust nickname for user: %s %s", user.Discord.ID, err.Error())
	}
}

func (m *Module) gameNicknameLoad() {
	m.gamesOnce.Do(func() {
		m.wg.Done()
	})
}

func (m *Module) mainNicknameLoad() {
	m.mainOnce.Do(func() {
		m.wg.Done()
	})
}

func (m *Module) mainRelationships(rel steamlang.EFriendRelationship, sid steamid.SteamId) {
	if v, ok := m.mainFriends.Load(sid); ok && v.(steamlang.EFriendRelationship) == rel {
		return
	}

	if rel == steamlang.EFriendRelationship_None {
		m.mainFriends.Delete(rel)
		return
	} else {
		m.mainFriends.Store(sid, rel)
	}

	logrus.Infof("MAIN: %s -> %s", sid, rel)

	id := utils.SteamIDToSteamID64(sid)
	res := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(context.Background(), bson.M{
		"steam.id": fmt.Sprint(id),
	})
	user := structures.User{}
	err := res.Err()
	if err == nil {
		err = res.Decode(&user)
	}
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return
		}

		logrus.Error("failed to fetch user from mongo: ", err)
		return
	}

	if err := m.adjustNickname(context.Background(), user, sectionMain); err != nil {
		logrus.Errorf("failed to adjust nickname for user: %s %s", user.Discord.ID, err.Error())
	}
}

const (
	sectionMain   = 1 << iota
	sectionGames  = 1 << iota
	forceNickname = 1 << iota
)

func (m *Module) adjustNickname(ctx context.Context, user structures.User, flags int) error {
	member, err := m.Ctx.Inst().Discord.Member(m.Ctx.Config().Discord.GuildID, user.Discord.ID)
	if err != nil {
		logrus.Error("unable to get member of user: ", err)
		return err
	}

	roleMp := map[string]bool{}
	for _, r := range member.Roles {
		roleMp[r] = true
	}

	isSpecial := false
	isSub := false
	for _, r := range m.Ctx.Config().Modules.Tracker.SpecialRoles {
		if roleMp[r] {
			isSpecial = true
			break
		}
	}
	if !isSpecial {
		for _, r := range m.Ctx.Config().Modules.Tracker.SubRoles {
			if roleMp[r] {
				isSub = true
				break
			}
		}
	}

	id, _ := strconv.ParseUint(user.Steam.ID, 10, 64)
	sid := utils.SteamID64ToSteamID(id)
	if v, ok := m.Games.Friends()[sid]; ok && flags&sectionGames != 0 {
		nick := m.Games.NicknameByID(sid)
		switch v.Relationship {
		case steamlang.EFriendRelationship_Friend:
			extra := ""
			goOn := true
			if subsGamesRegex.MatchString(nick) { // added because they are a sub
				extra = subsGamesRegex.FindAllStringSubmatch(nick, 1)[0][3]
			} else if specialGamesRegex.MatchString(nick) { // added because they are special
				extra = specialGamesRegex.FindAllStringSubmatch(nick, 1)[0][2]
			} else if flags&forceNickname == 0 { // added manually or has bad nickname
				// we cant unfriend them because we dont know who they are.
				logrus.Warnf("Unable to correct games nickname of user (%s) because their nickname is non-standard: %s", user.Discord.ID, m.Games.NicknameByID(sid))
				goOn = false
			}

			if goOn {
				newNick := ""
				// we must check their roles to make sure they are still subbed
				if isSpecial {
					// they are special now, we have to change their nickname to reflect this
					newNick = fmt.Sprintf("%s-%s", user.Twitch.Name, extra)
				} else if isSub {
					// we need to correct thier nickname
					// we can safely overwrite their nickname
					now := time.Now()
					currentYear, currentMonth, _ := now.Date()
					currentLocation := now.Location()
					firstOfMonth := time.Date(currentYear, currentMonth, 1, 0, 0, 0, 0, currentLocation)
					count, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameDotaGamePlayers).CountDocuments(context.Background(), bson.M{
						"user_id": user.ID,
						"created_at": bson.M{
							"$gte": firstOfMonth,
						},
					})
					if err != nil {
						logrus.Error("failed to get game count: ", err)
						return err
					}

					newNick = fmt.Sprintf("%dMC%s-%s", count, user.Twitch.Name, extra)
				} else {
					// they are no longer subbed we must remove them
					m.Games.RemoveFriend(sid)
				}
				if newNick != nick && (isSpecial || isSub) {
					m.Games.RenameFriend(sid, newNick)
				}
			}
		case steamlang.EFriendRelationship_RequestRecipient:
			if isSpecial || isSub {
				m.Games.AddFriend(sid)
			}
		}
	}

	if v, ok := m.Main.Friends()[sid]; ok && flags&sectionMain != 0 {
		nick := m.Main.NicknameByID(sid)
		switch v.Relationship {
		case steamlang.EFriendRelationship_Friend:
			extra := ""
			goOn := true
			if subsMainRegex.MatchString(nick) { // added because they are a sub
				extra = subsMainRegex.FindAllStringSubmatch(nick, 1)[0][2]
			} else if specialMainRegex.MatchString(nick) { // added because they are special
				extra = specialMainRegex.FindAllStringSubmatch(nick, 1)[0][2]
			} else if flags&forceNickname == 0 { // added manually or has bad nickname
				// we cant unfriend them because we dont know who they are.
				logrus.Warnf("Unable to correct main nickname of user (%s) because their nickname is non-standard: %s", user.Discord.ID, m.Games.NicknameByID(sid))
				goOn = false
			}

			if goOn {
				newNick := ""
				// we must check their roles to make sure they are still subbed
				if isSpecial {
					// they are special now, we have to change their nickname to reflect this
					newNick = fmt.Sprintf("%s-%s", user.Twitch.Name, extra)
				} else if isSub {
					// we need to correct thier nickname
					// we can safely overwrite their nickname
					newNick = fmt.Sprintf("MC%s-%s", user.Twitch.Name, extra)
				} else {
					// they are no longer subbed we must remove them
					m.Main.RemoveFriend(sid)
				}
				if newNick != nick && (isSpecial || isSub) {
					m.Main.RenameFriend(sid, newNick)
				}
			}
		case steamlang.EFriendRelationship_RequestRecipient:
			if isSpecial || isSub {
				m.Main.AddFriend(sid)
			}
		}
	}

	return nil
}

func (m *Module) autoAdjustNicknames() {
	first := true
	for {
		if !first {
			select {
			case <-time.After(time.Hour):
			case <-m.Ctx.Done():
				return
			}
		} else {
			first = false
		}
		logrus.Info("auto adjusting nicknames start")

		// we should reset all nicknames
		users := []structures.User{}
		cur, err := m.Ctx.Inst().Mongo.Collection(mongo.CollectionNameUsers).Find(context.Background(), bson.M{"steam.id": bson.M{
			"$type": "string",
		}})
		if err == nil {
			err = cur.All(context.Background(), &users)
		}
		if err != nil {
			logrus.Error("failed to fetch users: ", err)
			continue
		}
		for _, user := range users {
			if err := m.adjustNickname(context.Background(), user, sectionGames|sectionMain); err != nil {
				logrus.Errorf("failed to adjust nickname for user: %s %s", user.Discord.ID, err.Error())
			}
		}

		logrus.Info("auto adjusting nicknames done")
	}
}
