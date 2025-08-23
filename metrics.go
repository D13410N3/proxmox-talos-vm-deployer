package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vm_deployer_errors_total",
		Help: "Total number of VM deployer errors",
	}, []string{"handler"})

	createdCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vm_deployer_vm_created_total",
		Help: "Total number of VMs created",
	}, []string{"node", "base_template", "vm_template"})

	deletedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vm_deployer_vm_deleted_total",
		Help: "Total number of VMs deleted",
	}, []string{"node"})
)

func initMetrics() {
	prometheus.MustRegister(errorCounter, createdCounter, deletedCounter)
}

func incErrorCounterHandler(handler string) {
	errorCounter.With(prometheus.Labels{"handler": handler}).Inc()
}
