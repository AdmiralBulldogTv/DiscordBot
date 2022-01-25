package global

import "github.com/AdmiralBulldogTv/DiscordBot/src/instance"

type Instances struct {
	Redis      instance.Redis
	Mongo      instance.Mongo
	Prometheus instance.Prometheus
	Discord    instance.Discord
}
