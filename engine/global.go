package engine

import (
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/funkygao/gafka/ctx"
	"github.com/funkygao/gafka/zk"
	conf "github.com/funkygao/jsconf"
	log "github.com/funkygao/log4go"
)

var (
	availablePlugins = make(map[string]func() Plugin) // name:factory
	pluginTypeRegex  = regexp.MustCompile("^.*(Filter|Input|Output)$")

	// Globals returns the global configurations of dbus.
	// We export func instead of var to prevent the global var from being overwritten.
	Globals func() *GlobalConfig
)

// GlobalConfig is the struct for holding global config values.
type GlobalConfig struct {
	*conf.Conf
	stopping bool

	Zone    string
	Cluster string

	StartedAt      time.Time
	Debug          bool
	ClusterEnabled bool
	RouterTrack    bool
	WatchdogTick   time.Duration

	RPCPort int
	APIPort int

	ZrootConf       string
	ZrootCluster    string
	ZrootCheckpoint string

	InputRecyclePoolSize  int
	FilterRecyclePoolSize int
	HubChanSize           int
	PluginChanSize        int

	// registry is used to hold the global object shared between plugins.
	registry map[string]interface{}
	regMu    sync.RWMutex

	sigChan chan os.Signal
}

func (g *GlobalConfig) GetOrRegisterZkzone(zone string) *zk.ZkZone {
	g.regMu.Lock()
	defer g.regMu.Unlock()

	key := fmt.Sprintf("zkzone.%s", zone)
	if _, present := g.registry[key]; !present {
		zkzone := zk.NewZkZone(zk.DefaultConfig(zone, ctx.ZoneZkAddrs(zone)))
		g.registry[key] = zkzone
	}

	if z, ok := g.registry[key].(*zk.ZkZone); ok {
		return z
	}

	log.Critical("unknown zone: %s", zone)
	return nil
}

func DefaultGlobals() *GlobalConfig {
	return &GlobalConfig{
		APIPort:               9876,
		RPCPort:               9877,
		Debug:                 false,
		ClusterEnabled:        true,
		InputRecyclePoolSize:  100,
		FilterRecyclePoolSize: 100,
		HubChanSize:           200,
		PluginChanSize:        150,
		RouterTrack:           true,
		WatchdogTick:          time.Minute * 10,
		StartedAt:             time.Now(),
		registry:              map[string]interface{}{},
		ZrootConf:             "/dbus/conf",
		ZrootCheckpoint:       "/dbus/checkpoint",
		ZrootCluster:          "/dbus/cluster",
	}
}
