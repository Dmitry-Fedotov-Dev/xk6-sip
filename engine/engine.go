// Package engine is the SIP core: subscribers (Device), calls and their
// timings. It knows nothing about k6 or JavaScript; adapters translate its
// events into metrics and expose its objects to scripts.
package engine

import (
	"log/slog"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"golang.org/x/time/rate"
)

type Options struct {
	// LocalIP to bind SIP sockets to. Empty means the address the OS would
	// use to reach the device's proxy.
	LocalIP string
	// RegisterRate caps REGISTER requests per second across the engine.
	// Zero means unlimited.
	RegisterRate float64
	// ReadBufferSize is the per-socket SIP read buffer. sipgo's default of
	// 32 KB costs ~20 KB extra per subscriber; 4 KB fits UDP SIP messages.
	ReadBufferSize int
	// RingTimeout is how long an incoming call rings unclaimed or unanswered
	// before it is rejected with 480.
	RingTimeout time.Duration
	// TraceBodies keeps full messages in call traces, not only start lines.
	TraceBodies bool
	Observer    Observer
	Logger      *slog.Logger
}

var bufferSizeOnce sync.Once

type Engine struct {
	opts    Options
	log     *slog.Logger
	limiter *rate.Limiter

	mu      sync.Mutex
	devices map[*Device]struct{}
}

func New(opts Options) *Engine {
	if opts.ReadBufferSize == 0 {
		opts.ReadBufferSize = 4096
	}
	if opts.RingTimeout == 0 {
		opts.RingTimeout = 3 * time.Minute
	}
	if opts.Observer == nil {
		opts.Observer = NopObserver{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	// Process-wide setting in sipgo, read by every socket reader, so it is
	// set once by the first engine.
	bufferSizeOnce.Do(func() { sip.TransportBufferReadSize = uint16(opts.ReadBufferSize) })

	e := &Engine{
		opts:    opts,
		log:     opts.Logger,
		devices: make(map[*Device]struct{}),
	}
	if opts.RegisterRate > 0 {
		e.limiter = rate.NewLimiter(rate.Limit(opts.RegisterRate), 1)
	}
	return e
}

// Close destroys all devices: unregisters them and closes their sockets.
func (e *Engine) Close() {
	e.mu.Lock()
	devs := make([]*Device, 0, len(e.devices))
	for d := range e.devices {
		devs = append(devs, d)
	}
	e.mu.Unlock()

	var wg sync.WaitGroup
	for _, d := range devs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Destroy()
		}()
	}
	wg.Wait()
}

func (e *Engine) forget(d *Device) {
	e.mu.Lock()
	delete(e.devices, d)
	e.mu.Unlock()
}
