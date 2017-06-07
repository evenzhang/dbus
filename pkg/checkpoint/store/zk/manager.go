package zk

import (
	"path"
	"sort"

	"github.com/funkygao/dbus/pkg/checkpoint"
	"github.com/funkygao/dbus/pkg/checkpoint/state"
	"github.com/funkygao/gafka/zk"
)

var _ checkpoint.Manager = &manager{}

type manager struct {
	zkzone *zk.ZkZone
}

func NewManager(zkzone *zk.ZkZone, cluster string) checkpoint.Manager {
	if len(cluster) > 0 {
		root = zk.DbusCheckpointRoot(cluster)
	}
	return &manager{zkzone: zkzone}
}

func (m *manager) AllStates() ([]checkpoint.State, error) {
	schemes, _, err := m.zkzone.Conn().Children(root)
	if err != nil {
		return nil, err
	}

	sort.Strings(schemes)

	var r []checkpoint.State
	for _, scheme := range schemes {
		dsns, _, err := m.zkzone.Conn().Children(path.Join(root, scheme))
		if err != nil {
			return nil, err
		}

		sort.Strings(dsns)

		for _, dsn := range dsns {
			data, _, err := m.zkzone.Conn().Get(path.Join(root, scheme, dsn))
			if err != nil {
				return nil, err
			}

			s, err := state.Load(scheme, dsn, data)
			if err != nil {
				return nil, err
			}

			r = append(r, s)
		}
	}

	return r, nil
}
