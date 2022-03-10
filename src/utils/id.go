package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/Philipp15b/go-steam/v3/steamid"
)

var Epoch = time.Unix(1624968960, 0)

func NewID() int64 {
	now := int64(time.Since(Epoch)) / 100000
	n, _ := rand.Int(rand.Reader, big.NewInt(4194303)) // (1<<22)-1
	return (now << 22) | n.Int64()
}

func SteamIDToSteamID3(sid steamid.SteamId) uint64 {
	return uint64(sid.GetAccountId())
}

func SteamIDToSteamID64(sid steamid.SteamId) uint64 {
	return SteamID3ToSteamID64(uint64(sid.GetAccountId()))
}

func SteamID3ToSteamID(sid uint64) steamid.SteamId {
	return SteamID64ToSteamID(SteamID3ToSteamID64(sid))
}

func SteamID64ToSteamID(sid uint64) steamid.SteamId {
	sid -= 76561197960265728
	id, _ := steamid.NewId(fmt.Sprintf("STEAM_0:%d:%d", sid%2, sid/2))
	return id
}

func SteamID3ToSteamID64(sid uint64) uint64 {
	return sid + 76561197960265728
}

func SteamID64ToSteamID3(sid uint64) uint64 {
	return sid - 76561197960265728
}
