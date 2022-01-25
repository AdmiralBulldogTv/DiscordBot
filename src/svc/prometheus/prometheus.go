package prometheus

import (
	"github.com/AdmiralBulldogTv/DiscordBot/src/configure"
	"github.com/AdmiralBulldogTv/DiscordBot/src/instance"

	"github.com/prometheus/client_golang/prometheus"
)

type mon struct{}

func (m *mon) Register(r prometheus.Registerer) {
	r.MustRegister()
}

func LabelsFromKeyValue(kv []configure.KeyValue) prometheus.Labels {
	mp := prometheus.Labels{}

	for _, v := range kv {
		mp[v.Key] = v.Value
	}

	return mp
}

func New(opts SetupOptions) instance.Prometheus {
	return &mon{}
}

type SetupOptions struct {
	Labels prometheus.Labels
}
