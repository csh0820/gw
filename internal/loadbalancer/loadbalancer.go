package loadbalancer

import "github.com/csh0820/gw/internal/discovery"

type LoadBalancer interface {
	Select(instances []*discovery.ServiceInstance) *discovery.ServiceInstance
}
