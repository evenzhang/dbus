package zk

import (
	"github.com/funkygao/dbus/pkg/cluster"
	"github.com/funkygao/go-zookeeper/zk"
	log "github.com/funkygao/log4go"
	"github.com/funkygao/zkclient"
)

var (
	_ zkclient.ZkStateListener = &healthCheck{}
)

// healthCheck registers participant in zk ephemeral to allow other cluster components
// to detect failures.
// Right now our definition of health is fairly naive. If we register in zk we are healthy, otherwise
// we are dead.
type healthCheck struct {
	p cluster.Participant
	*zkclient.Client
	*keyBuilder
}

func newHealthCheck(p cluster.Participant, zc *zkclient.Client, kb *keyBuilder) *healthCheck {
	return &healthCheck{Client: zc, keyBuilder: kb, p: p}
}

func (h *healthCheck) startup() {
	h.SubscribeStateChanges(h)
	h.register()
}

func (h *healthCheck) close() {}

func (h *healthCheck) register() {
	if err := h.CreateLiveNode(h.participant(h.p.Endpoint), h.p.Marshal(), 3); err != nil {
		// 2 same participant running?
		panic(err)
	}

	log.Trace("[%s] come alive!", h.p)
}

func (h *healthCheck) HandleNewSession() (err error) {
	h.register()
	return
}

func (h *healthCheck) HandleStateChanged(state zk.State) (err error) {
	log.Trace("[%s] %s", h.p, state)
	return
}
