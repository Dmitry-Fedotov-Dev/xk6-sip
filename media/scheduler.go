package media

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Scheduler paces all sending streams from a few goroutines instead of one
// ticker per stream: with thousands of calls, per-stream tickers make the
// generator itself produce jitter. Streams are spread over shards; each
// shard ticks every 20 ms and sends one packet per stream.
type Scheduler struct {
	shards []*shard
	next   atomic.Uint64
	stop   chan struct{}
	once   sync.Once
}

type shard struct {
	mu      sync.Mutex
	streams []*Stream // copy-on-write, read without the lock by the ticker
	list    atomic.Pointer[[]*Stream]
}

// NewScheduler starts n shards; n <= 0 means GOMAXPROCS.
func NewScheduler(n int) *Scheduler {
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	s := &Scheduler{shards: make([]*shard, n), stop: make(chan struct{})}
	for i := range s.shards {
		sh := &shard{}
		sh.list.Store(&[]*Stream{})
		s.shards[i] = sh
		go sh.run(s.stop)
	}
	return s
}

var defaultScheduler = sync.OnceValue(func() *Scheduler { return NewScheduler(0) })

// Close stops the shard goroutines.
func (s *Scheduler) Close() { s.once.Do(func() { close(s.stop) }) }

func (s *Scheduler) add(st *Stream) *shard {
	sh := s.shards[s.next.Add(1)%uint64(len(s.shards))]
	sh.mu.Lock()
	sh.streams = append(sh.streams, st)
	l := append([]*Stream(nil), sh.streams...)
	sh.list.Store(&l)
	sh.mu.Unlock()
	return sh
}

func (sh *shard) remove(st *Stream) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	for i, x := range sh.streams {
		if x == st {
			sh.streams = append(sh.streams[:i], sh.streams[i+1:]...)
			break
		}
	}
	l := append([]*Stream(nil), sh.streams...)
	sh.list.Store(&l)
}

func (sh *shard) run(stop <-chan struct{}) {
	t := time.NewTicker(frameDurationMs * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			for _, st := range *sh.list.Load() {
				st.tick()
			}
		}
	}
}
