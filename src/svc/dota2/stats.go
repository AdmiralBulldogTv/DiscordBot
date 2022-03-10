package dota2

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/structures"
	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/mongo"
	"github.com/AdmiralBulldogTv/DiscordBot/src/utils"
	"github.com/paralin/go-dota2/protocol"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var bkbItem uint32 = 116

type GameWrapper struct {
	Game    structures.DotaGame
	Players []structures.DotaGamePlayer
}

func (c *DotaClient) QueryGames(gCtx global.Context, ctx context.Context, accID uint32, matchIDs []string) ([]GameWrapper, error) {
	matches := make([]*protocol.CMsgDOTAMatch, len(matchIDs))
	i := 0
	for _, v := range matchIDs {
		matchID, _ := strconv.ParseUint(v, 10, 64)
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		data, err := c.client.RequestMatchDetails(ctx, matchID)
		cancel()
		if err != nil {
			logrus.WithError(err).Errorf("failed to get match details for %d", matchID)
			continue
		}
		matches[i] = data.GetMatch()
		i++
		logrus.Debugf("fetched match details for %d", matchID)
		time.Sleep(time.Millisecond * 300) // delay so that we dont draw attention might have to increase this at some point
	}
	matches = matches[:i]
	if len(matches) == 0 {
		logrus.Debugln("no matches")
		return nil, nil
	}

	index := 0
	dbMatches := make([]GameWrapper, len(matches))
	steamPlayerIDsMp := map[string]bool{}

	for _, match := range matches {
		players := match.GetPlayers()
		team := 0
		win := false
		for _, player := range players {
			if player.GetAccountId() == accID {
				if player.GetPlayerSlot() > 100 {
					team = 2
					win = match.GetMatchOutcome() == protocol.EMatchOutcome_k_EMatchOutcome_DireVictory
				} else {
					team = 1
					win = match.GetMatchOutcome() == protocol.EMatchOutcome_k_EMatchOutcome_RadVictory
				}
				break
			}
		}
		if team == 0 {
			logrus.Warnf("match has bad id, %d, player not found", match.GetMatchId())
			continue
		}

		teammates := []structures.DotaGamePlayer{}
		gid := primitive.NewObjectIDFromTimestamp(time.Unix(int64(match.GetStartTime()), 0))
		for _, player := range players {
			if team == 1 {
				if player.GetPlayerSlot() > 50 {
					continue
				}
			} else {
				if player.GetPlayerSlot() < 50 {
					continue
				}
			}

			steamPlayerIDsMp[fmt.Sprint(player.GetAccountId())] = true

			id := utils.SteamID3ToSteamID64(uint64(player.GetAccountId()))
			dotaPlayer := structures.DotaGamePlayer{
				ID:         primitive.NewObjectIDFromTimestamp(time.Now()),
				SteamID:    fmt.Sprint(id),
				CreatedAt:  time.Unix(int64(match.GetStartTime()), 0),
				FetchedOn:  time.Now(),
				GameID:     gid,
				DotaGameID: fmt.Sprint(match.GetMatchId()),
			}

			stat := structures.DotaGamePlayerStats{}
			stat.Assists = int64(player.GetAssists())
			stat.BountyRunes += int64(player.GetBountyRunes())
			stat.Damage += int64(player.GetHeroDamage())
			var (
				totalTaken   int64
				totalReduced int64
			)
			damageRecieved := player.GetHeroDamageReceived()
			for _, v := range damageRecieved {
				totalReduced += int64(v.GetPreReduction() - v.GetPostReduction())
				totalTaken += int64(v.GetPostReduction())
			}
			stat.DamageTaken += int64(totalTaken)
			stat.DamageReduced += int64(totalReduced)
			stat.Deaths += int64(player.GetDeaths())
			stat.Denies += int64(player.GetDenies())
			stat.GPM += int64(player.GetGoldPerMin())
			stat.Healing += int64(player.GetHeroHealing())
			stat.Kills += int64(player.GetKills())
			stat.LastHits += int64(player.GetLastHits())
			stat.Levels += int64(player.GetLevel())
			stat.Networth += int64(player.GetNetWorth())
			stat.TowerDamage += int64(player.GetTowerDamage())
			stat.XPM += int64(player.GetXPPerMin())
			if player.GetItem_0() == bkbItem || player.GetItem_1() == bkbItem || player.GetItem_2() == bkbItem || player.GetItem_3() == bkbItem || player.GetItem_4() == bkbItem || player.GetItem_5() == bkbItem || player.GetItem_6() == bkbItem || player.GetItem_7() == bkbItem || player.GetItem_8() == bkbItem || player.GetItem_9() == bkbItem {
				stat.BKBs++
			}
			if win {
				stat.Wins++
			} else {
				stat.Losses++
			}

			dotaPlayer.Stats = stat
			teammates = append(teammates, dotaPlayer)
		}

		dbMatches[index] = GameWrapper{
			Game: structures.DotaGame{
				ID:        gid,
				GameID:    fmt.Sprint(match.GetMatchId()),
				Win:       win,
				CreatedAt: time.Unix(int64(match.GetStartTime()), 0),
				FetchedOn: time.Now(),
			},
			Players: teammates,
		}

		logrus.Debugf("found match %d", match.GetMatchId())

		index++
	}
	dbMatches = dbMatches[:index]

	steamPlayerIDs := []string{}

	for id := range steamPlayerIDsMp {
		steamPlayerIDs = append(steamPlayerIDs, id)
	}

	cur, err := gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).Find(ctx, bson.M{
		"steam.id": bson.M{
			"$in": steamPlayerIDs,
		},
	})
	users := []structures.User{}
	if err == nil {
		err = cur.All(ctx, &users)
	}
	if err != nil {
		return nil, err
	}

	userMp := map[string]structures.User{}
	for _, v := range users {
		userMp[v.Steam.ID] = v
	}

	for _, v := range dbMatches {
		for i, p := range v.Players {
			if _, ok := userMp[p.SteamID]; ok {
				p.UserID = userMp[p.SteamID].ID
				v.Players[i] = p
			}
		}
	}

	return dbMatches, nil
}

func (c *DotaClient) Done() <-chan struct{} {
	return c.done
}
