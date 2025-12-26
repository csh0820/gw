package loadbalancer

import (
	"sync/atomic"

	"github.com/csh0820/gw/internal/discovery"
)

type RoundRobin struct {
	counter uint64
}

func NewRoundRobin() *RoundRobin {
	return &RoundRobin{counter: 0}
}

func (rr *RoundRobin) Select(instances []*discovery.ServiceInstance) *discovery.ServiceInstance {
	if len(instances) == 0 {
		return nil
	}

	index := atomic.AddUint64(&rr.counter, 1) % uint64(len(instances))
	return instances[index]
}
