package xk6sip

import (
	"errors"
	"fmt"

	"github.com/grafana/sobek"
	"go.k6.io/k6/v2/js/common"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
)

// deviceKeys are the Device constructor fields that configure the
// subscriber; every other string field is an identity (ext, onk, gw_num...).
var deviceKeys = map[string]bool{
	"device": true, "registrar": true, "proxy": true, "user": true,
	"authUser": true, "pass": true, "password": true, "expires": true,
	"register": true, "displayName": true,
	"media": true, "codecs": true, "audio": true, "heardLevel": true,
}

// jsDevice is the script-facing Device:
//
//	const ua = new sip.Device({ device: 'phone1', registrar: 'sip:pbx:5060',
//	    user: 'a@domain', pass: 'secret', expires: 180, ext: '701' })
type jsDevice struct {
	ID string `js:"id"`

	mi  *ModuleInstance
	dev *engine.Device
	obs *vuObserver
}

func (mi *ModuleInstance) newDevice(call sobek.ConstructorCall) *sobek.Object {
	rt := mi.vu.Runtime()
	obj := objectArg(rt, call.Argument(0), "Device")

	cfg := engine.DeviceConfig{
		ID:          stringField(rt, obj, "device", ""),
		Registrar:   stringField(rt, obj, "registrar", ""),
		Proxy:       stringField(rt, obj, "proxy", ""),
		User:        stringField(rt, obj, "user", ""),
		AuthUser:    stringField(rt, obj, "authUser", ""),
		Password:    stringField(rt, obj, "pass", stringField(rt, obj, "password", "")),
		Expires:     durationField(rt, obj, "expires", 0),
		NoRegister:  !boolField(rt, obj, "register", true),
		DisplayName: stringField(rt, obj, "displayName", ""),
		Identities:  map[string]string{},
	}
	for _, k := range obj.Keys() {
		if deviceKeys[k] {
			continue
		}
		if v := obj.Get(k); isSet(v) {
			if s := v.String(); s != "" {
				cfg.Identities[k] = s
			}
		}
	}

	root := mi.root
	obs := &vuObserver{vu: mi.vu, m: root.metrics, deviceTag: root.opts.deviceTag}
	if mo, changed := mediaFields(rt, obj, root.opts.engine.Media); changed {
		cfg.Media = &mo
	}
	cfg.Observer = obs
	dev, err := root.getEngine().NewDevice(cfg)
	if err != nil {
		common.Throw(rt, fmt.Errorf("sip.Device: %w", err))
	}
	obs.device = dev.ID()
	d := &jsDevice{ID: dev.ID(), mi: mi, dev: dev, obs: obs}
	return rt.ToValue(d).ToObject(rt)
}

// start registers on first use inside an iteration. The device then lives
// until sip.shutdown() (call it in teardown) or the end of the process: k6
// v2 gives extensions no per-VU end-of-test hook, and vu.Context() ends
// with each iteration.
func (d *jsDevice) start() bool {
	rt := d.mi.vu.Runtime()
	if d.mi.vu.State() == nil {
		common.Throw(rt, errors.New("sip: devices can only be used inside a test function, not in the init context"))
	}
	if err := d.dev.Start(); err != nil {
		d.warn("start failed", err)
		return false
	}
	return true
}

func (d *jsDevice) warn(msg string, err error) {
	if st := d.mi.vu.State(); st != nil {
		st.Logger.WithField("device", d.ID).WithError(err).Warn("sip: " + msg)
	}
}

// Register sends REGISTER now (it is also sent automatically on first use).
func (d *jsDevice) Register() bool {
	if !d.dev.Started() {
		return d.start() && d.dev.Registered()
	}
	if err := d.dev.Register(); err != nil {
		d.warn("REGISTER failed", err)
		return false
	}
	return true
}

func (d *jsDevice) IsRegistered() bool { return d.dev.Registered() }

// Destroy unregisters and closes the device. It is done automatically when
// the VU finishes.
func (d *jsDevice) Destroy() { d.dev.Destroy() }

// Identity returns a configured number, e.g. ua.identity('ext').
func (d *jsDevice) Identity(key string) sobek.Value {
	rt := d.mi.vu.Runtime()
	if v, ok := d.dev.Identity(key); ok {
		return rt.ToValue(v)
	}
	return sobek.Undefined()
}

// Call starts an outgoing call and returns without waiting for an answer:
//
//	ua1.call({ callee: ua2, aon: 'ext' })  // dial ua2's 'ext' number
//	ua1.call({ callee: '702' })            // dial a literal number or URI
//	ua1.call({ aon: '*6701' })             // literal dial string
//
// Returns false if the device could not start (e.g. REGISTER failed).
func (d *jsDevice) Call(v sobek.Value) sobek.Value {
	rt := d.mi.vu.Runtime()
	obj := objectArg(rt, v, "call")
	target, err := dialTarget(rt, obj)
	if err != nil {
		common.Throw(rt, fmt.Errorf("call: %w", err))
	}
	opts := engine.CallOptions{
		Target:  target,
		Timeout: durationField(rt, obj, "timeout", 0),
		Label:   stringField(rt, obj, "id", ""),
		Headers: headersField(rt, obj),
	}
	if mo, changed := mediaFields(rt, obj, d.dev.MediaOptions()); changed {
		opts.Media = &mo
	}
	// The callee must be registered to receive the call.
	if callee := deviceArg(obj.Get("callee")); callee != nil && !callee.start() {
		return rt.ToValue(false)
	}
	if !d.start() {
		return rt.ToValue(false)
	}
	c, err := d.dev.Call(opts)
	if err != nil {
		d.warn("call failed", err)
		return rt.ToValue(false)
	}
	return rt.ToValue(newJSCall(d, c))
}

// ExpectCall waits for an incoming call and claims it:
//
//	ua2.expectCall({ caller: ua1, aon: 'ext', timeout: '10s' })
//
// Returns the call, or false if no matching call arrived in time.
func (d *jsDevice) ExpectCall(v sobek.Value) sobek.Value {
	rt := d.mi.vu.Runtime()
	obj := objectArg(rt, v, "expectCall")
	m, err := callerMatch(rt, obj)
	if err != nil {
		common.Throw(rt, fmt.Errorf("expectCall: %w", err))
	}
	timeout := durationField(rt, obj, "timeout", d.mi.root.opts.expectTimeout)
	if !d.start() {
		return rt.ToValue(false)
	}
	c, err := d.dev.ExpectCall(m, timeout)
	if err != nil {
		d.warn("expectCall failed", err)
	}
	if c == nil {
		d.obs.expectFailed("call")
		return rt.ToValue(false)
	}
	return rt.ToValue(newJSCall(d, c))
}

func deviceArg(v sobek.Value) *jsDevice {
	if !isSet(v) {
		return nil
	}
	d, _ := v.Export().(*jsDevice)
	return d
}

// dialTarget resolves {callee, aon} to what is dialled. With a callee
// device, aon names one of its identities (default 'ext'); otherwise aon is
// a literal dial string.
func dialTarget(rt *sobek.Runtime, obj *sobek.Object) (string, error) {
	aon := stringField(rt, obj, "aon", "")
	calleeV := obj.Get("callee")
	if callee := deviceArg(calleeV); callee != nil {
		key := aon
		if key == "" {
			key = "ext"
		}
		if num, ok := callee.dev.Identity(key); ok {
			return num, nil
		}
		if aon == "" {
			return "", fmt.Errorf("callee %s has no 'ext'; pass aon", callee.ID)
		}
		return "", fmt.Errorf("callee %s has no identity %q", callee.ID, aon)
	}
	if isSet(calleeV) {
		return calleeV.String(), nil
	}
	if aon != "" {
		return aon, nil
	}
	return "", errors.New("callee or aon is required")
}

// callerMatch resolves {caller, aon} to the expected caller number. With a
// caller device, aon names one of its identities; without aon any caller
// matches. A string caller or a literal aon is matched as is.
func callerMatch(rt *sobek.Runtime, obj *sobek.Object) (engine.CallMatch, error) {
	var m engine.CallMatch
	aon := stringField(rt, obj, "aon", "")
	callerV := obj.Get("caller")
	if caller := deviceArg(callerV); caller != nil {
		m.CallerDevice = caller.dev
		if aon == "" {
			return m, nil
		}
		num, ok := caller.dev.Identity(aon)
		if !ok {
			return m, fmt.Errorf("caller %s has no identity %q", caller.ID, aon)
		}
		m.Caller = num
		return m, nil
	}
	if isSet(callerV) {
		m.Caller = callerV.String()
		return m, nil
	}
	m.Caller = aon
	return m, nil
}

func headersField(rt *sobek.Runtime, obj *sobek.Object) map[string]string {
	v := obj.Get("headers")
	if !isSet(v) {
		return nil
	}
	h := map[string]string{}
	ho := objectArg(rt, v, "headers")
	for _, k := range ho.Keys() {
		h[k] = ho.Get(k).String()
	}
	return h
}
