package mysql

import (
	"fmt"
	"sync"
	"time"

	"github.com/funkygao/dbus/engine"
	"github.com/funkygao/dbus/pkg/cluster"
	"github.com/funkygao/dbus/pkg/myslave"
	log "github.com/funkygao/log4go"
)

func (this *MysqlbinlogInput) runClustered(r engine.InputRunner, h engine.PluginHelper) error {
	name := r.Name()
	backoff := time.Second * 5
	ex := r.Exchange()
	stopper := r.Stopper()

	globals := engine.Globals()
	var myResources []cluster.Resource
	resourcesCh := r.Resources()

	reapSlaves := func(wg *sync.WaitGroup, stopper chan<- struct{}) {
		close(stopper)
		wg.Wait()
	}

	for {
	RESTART_REPLICATION:

		// wait till got some resource
		for {
			if len(myResources) != 0 {
				log.Trace("[%s] bingo! %d: %+v", name, len(myResources), myResources)
				break
			}

			log.Trace("[%s] awaiting resources", name)

			select {
			case <-stopper:
				log.Debug("[%s] yes sir!", name)
				return nil
			case myResources = <-resourcesCh:
			}
		}

		var wg sync.WaitGroup
		slavesStopper := make(chan struct{})
		replicationErrs := make(chan error, 5)

		// got new resources assignment!

		this.mu.Lock()
		for _, resource := range myResources {
			dsn := resource.DSN()
			slave := myslave.New(name, dsn, globals.ZrootCheckpoint).LoadConfig(this.cf)
			this.slaves[dsn] = slave

			wg.Add(1)
			go this.runSlaveReplication(slave, name, ex, &wg, slavesStopper, replicationErrs)
		}
		this.mu.Unlock()

		for {
			select {
			case <-stopper:
				reapSlaves(&wg, slavesStopper)
				return nil

			case myResources = <-resourcesCh:
				log.Trace("[%s] cluster rebalanced, restart replication", name)
				reapSlaves(&wg, slavesStopper)
				goto RESTART_REPLICATION

			case err := <-replicationErrs:
				// e,g.
				// ERROR 1236 (HY000): Could not find first log file name in binary log index file
				// ERROR 1236 (HY000): Could not open log file
				// ERROR 1045 (28000): Access denied for user 'test'@'10.1.1.1'
				// read initial handshake error, caused by Too many connections

				// myResources not changed, so next round still consume the same resources

				// FIXME if err is 'Access denied', this resource will be de facto orphan: it will never succeed
				log.Error("[%s] backoff %s and restart replication: %s", name, backoff, err)
				select {
				case <-time.After(backoff):
				case <-stopper:
					reapSlaves(&wg, slavesStopper)
					return nil
				}
				goto RESTART_REPLICATION
			}
		}
	}

	return nil
}

func (this *MysqlbinlogInput) runSlaveReplication(slave *myslave.MySlave, name string, ex engine.Exchange,
	wg *sync.WaitGroup, slavesStopper <-chan struct{}, replicationErrs chan<- error) {
	defer func() {
		log.Trace("[%s] stopping replication from %s", name, slave.DSN())

		slave.StopReplication()
		wg.Done()
	}()

	if img, err := slave.BinlogRowImage(); err != nil {
		// e,g.
		// ERROR 1045 (28000): Access denied for user 'test'@'10.1.1.1'
		log.Error("[%s] %v", name, err)
	} else {
		log.Debug("[%s] binlog row image=%s", name, img)
	}

	log.Trace("[%s] starting replication from %s", name, slave.DSN())

	ready := make(chan struct{})
	go slave.StartReplication(ready)
	select {
	case <-ready:
	case <-slavesStopper:
		return
	}

	rows := slave.Events()
	replErrors := slave.Errors()
	dsn := slave.DSN()
	for {
		select {
		case <-slavesStopper:
			return

		case err, ok := <-replErrors:
			// e,g.
			// ERROR 1045 (28000): Access denied for user 'test'@'10.1.1.1'
			if ok {
				log.Error("[%s] %v, stop from %s", name, err, dsn)
				this.tryAutoHeal(name, err, slave)
				replicationErrs <- err
			}
			return

		case pack := <-ex.InChan():
			select {
			case <-slavesStopper:
				return

			case err, ok := <-replErrors:
				if ok {
					log.Error("[%s] %v, stop from %s", name, err, dsn)
					this.tryAutoHeal(name, err, slave)
					replicationErrs <- err
				} else {
					log.Error("[%s] error stream closed from %s", name, dsn)
					replicationErrs <- fmt.Errorf("[%s] error stream closed from %s", name, slave.DSN())
				}
				return

			case row, ok := <-rows:
				if !ok {
					log.Error("[%s] event stream closed from %s", name, dsn)
					return
				}

				if row.Length() < this.maxEventLength {
					pack.Payload = row
					pack.Metadata = dsn
					ex.Emit(pack)
				} else {
					// TODO this.slave.MarkAsProcessed(r), also consider batcher partial failure
					log.Warn("[%s] %s ignored len=%d %s", name, dsn, row.Length(), row.MetaInfo())
					pack.Recycle()
				}
			}
		}
	}
}
