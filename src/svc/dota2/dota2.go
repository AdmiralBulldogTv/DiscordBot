package dota2

import (
	"context"
	"time"

	"github.com/AdmiralBulldogTv/DiscordBot/src/svc/steam"
	"github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/events"
	"github.com/paralin/go-dota2/protocol"
	"github.com/sirupsen/logrus"
)

const dota2GameID = 570

type DotaClient struct {
	client  *dota2.Dota2
	sclient *steam.Client
	done    chan struct{}
	online  bool
}

func New(ctx context.Context, details steam.AccDetails) *DotaClient {
	dotaClient := &DotaClient{
		done: make(chan struct{}),
	}

	dotaClient.sclient = steam.NewClient(ctx, &steam.Config{
		Details: details,
		OnLogin: func() {
			dotaClient.sclient.Raw().GC.SetGamesPlayed(dota2GameID)
		},
		OnDisconnect: func() {
			dotaClient.online = false
		},
		OnRawEvent: func(ev interface{}) {
			stateChanged, ok := ev.(events.ClientStateChanged)
			if !ok {
				return
			}

			newOnline := stateChanged.NewState.ConnectionStatus == protocol.GCConnectionStatus_GCConnectionStatus_HAVE_SESSION
			if newOnline != dotaClient.online {
				dotaClient.online = newOnline
			}
		},
	})

	dotaClient.client = dota2.New(dotaClient.sclient.Raw(), logrus.StandardLogger())

	go func() {
		<-ctx.Done()
		dotaClient.client.SetPlaying(false)
		<-dotaClient.sclient.Done()
		close(dotaClient.done)
	}()

	go func() {
		tick := time.NewTicker(time.Second * 10)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if dotaClient.sclient.Raw().Connected() {
					if !dotaClient.online {
						logrus.Info("dota connected")
						dotaClient.client.SetPlaying(true)
						dotaClient.client.SayHello()
					}
				} else {
					logrus.Info("dota disconnected")
				}
			}
		}
	}()

	return dotaClient
}

func (c *DotaClient) Ready() bool {
	return c.online
}
