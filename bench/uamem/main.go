// uamem measures the memory and goroutine cost of N sipgo subscribers
// (UA + Client + Server sharing one UDP socket), as used in the
// "subscriber per VU" mode.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type subscriber struct {
	port int
	ua   *sipgo.UserAgent
	cl   *sipgo.Client
	srv  *sipgo.Server
}

func newSubscriber(ctx context.Context, ip string) (*subscriber, error) {
	// Reserve a free port first so client and server share it.
	pc, err := net.ListenPacket("udp", ip+":0")
	if err != nil {
		return nil, err
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port

	ua, err := sipgo.NewUA(sipgo.WithUserAgentHostname(ip))
	if err != nil {
		return nil, err
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	cl, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname(ip),
		sipgo.WithClientPort(port),
		// Send from the listener socket; WithClientPort alone only sets the Via port.
		sipgo.WithClientConnectionAddr(fmt.Sprintf("%s:%d", ip, port)))
	if err != nil {
		return nil, err
	}
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	go srv.ServeUDP(pc)
	return &subscriber{port: port, ua: ua, cl: cl, srv: srv}, nil
}

func heapMB() (float64, float64) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapInuse) / 1e6, float64(m.Sys) / 1e6
}

func main() {
	n := flag.Int("n", 1000, "subscribers")
	buf := flag.Int("buf", 32768, "sip.TransportBufferReadSize")
	ping := flag.Bool("ping", false, "each subscriber sends OPTIONS to its neighbour")
	flag.Parse()
	sip.TransportBufferReadSize = uint16(*buf)

	ctx := context.Background()
	h0, s0 := heapMB()
	g0 := runtime.NumGoroutine()

	subs := make([]*subscriber, 0, *n)
	start := time.Now()
	for i := 0; i < *n; i++ {
		s, err := newSubscriber(ctx, "127.0.0.1")
		if err != nil {
			fmt.Fprintf(os.Stderr, "subscriber %d: %v\n", i, err)
			break
		}
		subs = append(subs, s)
	}
	elapsed := time.Since(start)
	time.Sleep(500 * time.Millisecond)

	h1, s1 := heapMB()
	g1 := runtime.NumGoroutine()
	k := float64(len(subs))
	fmt.Printf("subs=%d buf=%d create=%v\n", len(subs), *buf, elapsed.Round(time.Millisecond))
	fmt.Printf("heap_inuse: %.1f MB total, %.1f KB/sub\n", h1-h0, (h1-h0)*1e3/k)
	fmt.Printf("sys:        %.1f MB total, %.1f KB/sub\n", s1-s0, (s1-s0)*1e3/k)
	fmt.Printf("goroutines: %d total, %.2f /sub\n", g1-g0, float64(g1-g0)/k)
	if *ping {
		pingAll(ctx, subs)
		h2, s2 := heapMB()
		fmt.Printf("after ping heap_inuse: %.1f KB/sub, sys: %.1f KB/sub, goroutines: %.2f /sub\n",
			(h2-h0)*1e3/k, (s2-s0)*1e3/k, float64(runtime.NumGoroutine()-g0)/k)
	}
	runtime.KeepAlive(subs)
}

// pingAll sends OPTIONS from every subscriber to the next one, concurrently,
// and checks that the reply arrives on the subscriber's own listening port.
func pingAll(ctx context.Context, subs []*subscriber) {
	var ok, fail atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, 500)
	start := time.Now()
	for i, s := range subs {
		dst := subs[(i+1)%len(subs)]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req := sip.NewRequest(sip.OPTIONS, sip.Uri{Host: "127.0.0.1", Port: dst.port})
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			res, err := s.cl.Do(cctx, req)
			via := req.Via()
			if err != nil || res.StatusCode != 200 || via == nil || via.Port != s.port {
				if fail.Add(1) == 1 {
					fmt.Fprintf(os.Stderr, "ping fail: err=%v via=%v want port %d\n", err, via, s.port)
				}
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	el := time.Since(start)
	fmt.Printf("ping: ok=%d fail=%d in %v (%.0f req/s)\n", ok.Load(), fail.Load(), el.Round(time.Millisecond), float64(ok.Load())/el.Seconds())
}
