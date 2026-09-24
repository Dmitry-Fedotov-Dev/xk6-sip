package xk6sip

import (
	"strconv"
	"time"

	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/metrics"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
)

type sipMetrics struct {
	requests         *metrics.Metric
	requestDuration  *metrics.Metric
	failedRequests   *metrics.Metric
	callSetupTime    *metrics.Metric
	postDialDelay    *metrics.Metric
	callSuccess      *metrics.Metric
	callDuration     *metrics.Metric
	inviteDelivery   *metrics.Metric
	registrations    *metrics.Metric
	expectedFailures *metrics.Metric
	rtpSent          *metrics.Metric
	rtpReceived      *metrics.Metric
	rtpLost          *metrics.Metric
	rtpJitter        *metrics.Metric
	rtpAudioHeard    *metrics.Metric
}

func registerMetrics(reg *metrics.Registry) (*sipMetrics, error) {
	m := &sipMetrics{}
	var err error
	for _, d := range []struct {
		dst  *(*metrics.Metric)
		name string
		typ  metrics.MetricType
		vt   metrics.ValueType
	}{
		{&m.requests, "sip_requests", metrics.Counter, metrics.Default},
		{&m.requestDuration, "sip_request_duration", metrics.Trend, metrics.Time},
		{&m.failedRequests, "sip_failed_requests", metrics.Rate, metrics.Default},
		{&m.callSetupTime, "sip_call_setup_time", metrics.Trend, metrics.Time},
		{&m.postDialDelay, "sip_post_dial_delay", metrics.Trend, metrics.Time},
		{&m.callSuccess, "sip_call_success", metrics.Rate, metrics.Default},
		{&m.callDuration, "sip_call_duration", metrics.Trend, metrics.Time},
		{&m.inviteDelivery, "sip_invite_delivery_time", metrics.Trend, metrics.Time},
		{&m.registrations, "sip_registrations", metrics.Counter, metrics.Default},
		{&m.expectedFailures, "sip_expect_failed", metrics.Counter, metrics.Default},
		{&m.rtpSent, "rtp_packets_sent", metrics.Counter, metrics.Default},
		{&m.rtpReceived, "rtp_packets_received", metrics.Counter, metrics.Default},
		{&m.rtpLost, "rtp_packets_lost", metrics.Counter, metrics.Default},
		{&m.rtpJitter, "rtp_jitter", metrics.Trend, metrics.Time},
		{&m.rtpAudioHeard, "rtp_audio_heard", metrics.Rate, metrics.Default},
	} {
		if *d.dst, err = reg.NewMetric(d.name, d.typ, d.vt); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// vuObserver pushes engine events of one VU's devices into that VU's
// sample channel. Events may arrive between iterations (re-REGISTER); they
// are dropped once the VU is done.
type vuObserver struct {
	vu        modules.VU
	m         *sipMetrics
	device    string
	deviceTag bool
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func (o *vuObserver) push(m *metrics.Metric, value float64, tags map[string]string) {
	state := o.vu.State()
	if state == nil {
		return // init context
	}
	ts := state.Tags.GetCurrentValues().Tags
	if o.deviceTag {
		ts = ts.With("device", o.device)
	}
	for k, v := range tags {
		ts = ts.With(k, v)
	}
	metrics.PushIfNotDone(o.vu.Context(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{Metric: m, Tags: ts},
		Time:       time.Now(),
		Value:      value,
	})
}

func (o *vuObserver) Request(e engine.RequestEvent) {
	status := strconv.Itoa(e.Status)
	if e.Status == 0 {
		status = "timeout"
	}
	tags := map[string]string{"method": e.Method, "status": status}
	o.push(o.m.requests, 1, tags)
	if e.Status != 0 {
		o.push(o.m.requestDuration, ms(e.Duration), tags)
	}
	failed := 0.0
	if e.Failed() {
		failed = 1
	}
	o.push(o.m.failedRequests, failed, map[string]string{"method": e.Method})
	if e.Method == "REGISTER" && e.Status == 200 {
		o.push(o.m.registrations, 1, nil)
	}
}

func (o *vuObserver) CallSetup(e engine.CallSetupEvent) {
	if e.Cancelled && !e.Success {
		return // the script hung up before answer; neither success nor failure
	}
	ok := 0.0
	if e.Success {
		ok = 1
		o.push(o.m.callSetupTime, ms(e.SetupTime), nil)
	}
	if e.HasPDD {
		o.push(o.m.postDialDelay, ms(e.PDD), nil)
	}
	o.push(o.m.callSuccess, ok, map[string]string{"status": strconv.Itoa(e.Status)})
}

func (o *vuObserver) CallEnd(e engine.CallEndEvent) {
	if e.Direction == engine.Outgoing {
		o.push(o.m.callDuration, ms(e.Duration), map[string]string{"ended_by": string(e.EndedBy)})
	}
}

func (o *vuObserver) IncomingCall(e engine.IncomingCallEvent) {
	if e.HasDelivery {
		o.push(o.m.inviteDelivery, ms(e.Delivery), nil)
	}
}

func (o *vuObserver) expectFailed(what string) {
	o.push(o.m.expectedFailures, 1, map[string]string{"expect": what})
}

// Media is reported once per call leg when a connected call ends.
// rtp_audio_heard is the share of legs that received any audio: below 1
// means one-way or no audio.
func (o *vuObserver) Media(e engine.MediaEvent) {
	st := e.Stats
	tags := map[string]string{"codec": st.Codec, "direction": e.Direction.String()}
	o.push(o.m.rtpSent, float64(st.PacketsSent), tags)
	o.push(o.m.rtpReceived, float64(st.PacketsReceived), tags)
	o.push(o.m.rtpLost, float64(st.PacketsLost), tags)
	if st.PacketsReceived > 1 {
		o.push(o.m.rtpJitter, ms(st.Jitter), tags)
	}
	heard := 0.0
	if st.Heard > 0 {
		heard = 1
	}
	o.push(o.m.rtpAudioHeard, heard, tags)
}
