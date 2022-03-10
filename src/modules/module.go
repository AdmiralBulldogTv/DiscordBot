package modules

import (
	"github.com/AdmiralBulldogTv/DiscordBot/src/global"
	"github.com/AdmiralBulldogTv/DiscordBot/src/modules/common"
	"github.com/AdmiralBulldogTv/DiscordBot/src/modules/goodnight"
	"github.com/AdmiralBulldogTv/DiscordBot/src/modules/inhouse"
	"github.com/AdmiralBulldogTv/DiscordBot/src/modules/points"
	"github.com/AdmiralBulldogTv/DiscordBot/src/modules/tracker"
	"github.com/sirupsen/logrus"
)

type Module interface {
	Name() string
	Register(gCtx global.Context) (<-chan struct{}, error)
}

func New(gCtx global.Context) <-chan struct{} {
	done := make(chan struct{})

	modules := []Module{}
	if gCtx.Config().Modules.Points.Enabled {
		modules = append(modules, points.New())
	}
	if gCtx.Config().Modules.Common.Enabled {
		modules = append(modules, common.New())
	}
	if gCtx.Config().Modules.GoodNight.Enabled {
		modules = append(modules, goodnight.New())
	}
	if gCtx.Config().Modules.InHouse.Enabled {
		modules = append(modules, inhouse.New())
	}
	if gCtx.Config().Modules.Tracker.Enabled {
		modules = append(modules, tracker.New())
	}
	dones := []<-chan struct{}{}

	for _, module := range modules {
		d, err := module.Register(gCtx)
		if err != nil {
			logrus.Error("failed to load: ", module.Name())
			continue
		}
		dones = append(dones, d)
	}

	go func() {
		<-gCtx.Done()

		for _, d := range dones {
			<-d
		}

		close(done)
	}()

	return done
}
