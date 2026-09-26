package xk6sip

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// processExporter serves resource usage of this k6 process for Prometheus
// to scrape: CPU, memory and goroutines (standard process and Go
// collectors) plus SIP/RTP traffic of all devices. k6's own metrics only
// describe the test, not the process running it.
type processExporter struct {
	once sync.Once
	addr string
	err  error
}

// start listens on addr once per process; later calls with the same addr
// are no-ops, a different addr is an error.
func (x *processExporter) start(addr string, r *RootModule) error {
	x.once.Do(func() {
		x.addr = addr
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			x.err = fmt.Errorf("sip.options: metricsAddr: %w", err)
			return
		}
		srv := &http.Server{Handler: exporterHandler(r), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
	})
	if x.err == nil && addr != x.addr {
		return errors.New("sip.options: metricsAddr cannot change once the exporter is running")
	}
	return x.err
}

func exporterHandler(r *RootModule) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())

	traffic := []struct {
		proto, dir, unit string
		value            func() uint64
	}{
		{"sip", "in", "bytes", func() uint64 { return engine.SIPNetStats().BytesIn }},
		{"sip", "out", "bytes", func() uint64 { return engine.SIPNetStats().BytesOut }},
		{"sip", "in", "packets", func() uint64 { return engine.SIPNetStats().PacketsIn }},
		{"sip", "out", "packets", func() uint64 { return engine.SIPNetStats().PacketsOut }},
		{"rtp", "in", "bytes", func() uint64 { return media.RTPNetStats().BytesIn }},
		{"rtp", "out", "bytes", func() uint64 { return media.RTPNetStats().BytesOut }},
		{"rtp", "in", "packets", func() uint64 { return media.RTPNetStats().PacketsIn }},
		{"rtp", "out", "packets", func() uint64 { return media.RTPNetStats().PacketsOut }},
	}
	for _, t := range traffic {
		reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name:        "xk6sip_network_" + t.unit + "_total",
			Help:        "UDP " + t.unit + " on the SIP and RTP sockets of this k6 process (payload, without IP/UDP headers).",
			ConstLabels: prometheus.Labels{"proto": t.proto, "direction": t.dir},
		}, func() float64 { return float64(t.value()) }))
	}

	counts := func() (int, int) {
		r.mu.Lock()
		e := r.engine
		r.mu.Unlock()
		if e == nil {
			return 0, 0
		}
		return e.Counts()
	}
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "xk6sip_devices", Help: "SIP devices with an open socket.",
	}, func() float64 { d, _ := counts(); return float64(d) }))
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "xk6sip_calls_in_progress", Help: "Answered outgoing calls that have not ended.",
	}, func() float64 { _, c := counts(); return float64(c) }))

	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
