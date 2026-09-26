// Package xk6sip is the k6 extension k6/x/sip. It exposes the SIP engine to
// k6 scripts and turns engine events into k6 metrics.
package xk6sip

import (
	"errors"
	"sync"
	"time"

	"github.com/grafana/sobek"
	"go.k6.io/k6/v2/js/common"
	"go.k6.io/k6/v2/js/modules"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
)

func init() {
	modules.Register("k6/x/sip", New())
}

// RootModule is shared by all VUs; it owns the single SIP engine of the
// process so that engine-wide limits (register rate) apply to the whole test.
type RootModule struct {
	mu       sync.Mutex
	opts     moduleOptions
	engine   *engine.Engine
	metrics  *sipMetrics
	audio    audioCache
	exporter processExporter
}

type moduleOptions struct {
	engine        engine.Options
	expectTimeout time.Duration
	deviceTag     bool
}

func New() *RootModule {
	return &RootModule{opts: moduleOptions{expectTimeout: 30 * time.Second}}
}

func (r *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.metrics == nil {
		m, err := registerMetrics(vu.InitEnv().Registry)
		if err != nil {
			common.Throw(vu.Runtime(), err)
		}
		r.metrics = m
	}
	return &ModuleInstance{root: r, vu: vu}
}

// getEngine creates the engine on first use, freezing options.
func (r *RootModule) getEngine() *engine.Engine {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		r.engine = engine.New(r.opts.engine)
	}
	return r.engine
}

type ModuleInstance struct {
	root *RootModule
	vu   modules.VU
}

func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{Named: map[string]any{
		"Device":   mi.newDevice,
		"options":  mi.options,
		"shutdown": mi.shutdown,
		"audio":    mi.audio,
		"tone":     mi.tone,
	}}
}

// shutdown hangs up, unregisters and closes every device of this k6
// process. Call it from teardown(), which runs once after all VUs are done:
//
//	export function teardown() { sip.shutdown(); }
func (mi *ModuleInstance) shutdown() {
	r := mi.root
	r.mu.Lock()
	e := r.engine
	r.mu.Unlock()
	if e != nil {
		e.Close()
	}
}

// options sets engine-wide options. It must be called in the init context
// before the first device is used; later calls with different values fail.
//
//	sip.options({ localIP: '10.0.0.5', registerRate: 50, ringTimeout: '3m',
//	              expectTimeout: '30s', trace: false, deviceTag: false,
//	              metricsAddr: '127.0.0.1:6566' })
func (mi *ModuleInstance) options(v sobek.Value) {
	rt := mi.vu.Runtime()
	obj := objectArg(rt, v, "options")
	r := mi.root
	r.mu.Lock()
	defer r.mu.Unlock()

	o := r.opts
	o.engine.LocalIP = stringField(rt, obj, "localIP", o.engine.LocalIP)
	o.engine.RegisterRate = floatField(rt, obj, "registerRate", o.engine.RegisterRate)
	o.engine.RingTimeout = durationField(rt, obj, "ringTimeout", o.engine.RingTimeout)
	o.engine.TraceBodies = boolField(rt, obj, "trace", o.engine.TraceBodies)
	o.expectTimeout = durationField(rt, obj, "expectTimeout", o.expectTimeout)
	o.deviceTag = boolField(rt, obj, "deviceTag", o.deviceTag)
	o.engine.Media, _ = mediaFields(rt, obj, o.engine.Media)

	if r.engine != nil && !sameEngineOptions(o.engine, r.opts.engine) {
		common.Throw(rt, errors.New("sip.options: engine options cannot change after the first device was used"))
	}
	r.opts = o

	// Resource usage of the k6 process for Prometheus; started once, in the
	// init context of the first VU.
	if addr := stringField(rt, obj, "metricsAddr", ""); addr != "" {
		if err := r.exporter.start(addr, r); err != nil {
			common.Throw(rt, err)
		}
	}
}

func sameEngineOptions(a, b engine.Options) bool {
	return a.LocalIP == b.LocalIP && a.RegisterRate == b.RegisterRate && a.RingTimeout == b.RingTimeout &&
		a.TraceBodies == b.TraceBodies && sameMedia(a.Media, b.Media)
}
