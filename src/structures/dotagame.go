package structures

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type DotaGame struct {
	ID        primitive.ObjectID `bson:"_id"`
	GameID    string             `bson:"game_id"`
	Win       bool               `bson:"win"`
	CreatedAt time.Time          `bson:"created_at"`
	FetchedOn time.Time          `bson:"fetched_on"`
}

type DotaGamePlayer struct {
	ID         primitive.ObjectID  `bson:"_id"`
	GameID     primitive.ObjectID  `bson:"game_id"`
	DotaGameID string              `bson:"dota_game_id"`
	SteamID    string              `bson:"steam_id"`
	UserID     primitive.ObjectID  `bson:"user_id"`
	Stats      DotaGamePlayerStats `bson:"stats"`
	CreatedAt  time.Time           `bson:"created_at"`
	FetchedOn  time.Time           `bson:"fetched_on"`
}

type DotaGamePlayerStats struct {
	Wins          int64 `bson:"wins"`
	Losses        int64 `bson:"losses"`
	Kills         int64 `bson:"kills"`
	Assists       int64 `bson:"assists"`
	Deaths        int64 `bson:"deaths"`
	GPM           int64 `bson:"gpm"`
	XPM           int64 `bson:"xpm"`
	LastHits      int64 `bson:"last_hits"`
	Denies        int64 `bson:"denies"`
	Networth      int64 `bson:"networth"`
	Healing       int64 `bson:"healing"`
	Damage        int64 `bson:"damage"`
	DamageTaken   int64 `bson:"damage_taken"`
	DamageReduced int64 `bson:"damage_reduced"`
	Levels        int64 `bson:"levels"`
	BountyRunes   int64 `bson:"bounty_runes"`
	BKBs          int64 `bson:"bkbs"`
	TowerDamage   int64 `bson:"tower_damage"`
}
