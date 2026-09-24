package xk6sip

import (
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/sobek"
	"go.k6.io/k6/v2/js/common"
)

func isSet(v sobek.Value) bool {
	return v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v)
}

func objectArg(rt *sobek.Runtime, v sobek.Value, what string) *sobek.Object {
	if !isSet(v) {
		return rt.NewObject()
	}
	obj, ok := v.(*sobek.Object)
	if !ok {
		common.Throw(rt, fmt.Errorf("%s: expected an object, got %s", what, v.String()))
	}
	return obj
}

func stringField(rt *sobek.Runtime, obj *sobek.Object, name, def string) string {
	v := obj.Get(name)
	if !isSet(v) {
		return def
	}
	return v.String()
}

func boolField(rt *sobek.Runtime, obj *sobek.Object, name string, def bool) bool {
	v := obj.Get(name)
	if !isSet(v) {
		return def
	}
	return v.ToBoolean()
}

func floatField(rt *sobek.Runtime, obj *sobek.Object, name string, def float64) float64 {
	v := obj.Get(name)
	if !isSet(v) {
		return def
	}
	return v.ToFloat()
}

// toDuration accepts "10s"-style strings or numbers of seconds, like k6's
// sleep().
func toDuration(rt *sobek.Runtime, v sobek.Value, what string) time.Duration {
	switch x := v.Export().(type) {
	case string:
		// CSV fields arrive as strings: "180" means seconds.
		if secs, err := strconv.ParseFloat(x, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
		d, err := time.ParseDuration(x)
		if err != nil {
			common.Throw(rt, fmt.Errorf("%s: %w", what, err))
		}
		return d
	case int64:
		return time.Duration(x) * time.Second
	case float64:
		return time.Duration(x * float64(time.Second))
	default:
		common.Throw(rt, fmt.Errorf("%s: expected a duration like '10s' or seconds, got %s", what, v.String()))
		return 0
	}
}

func durationField(rt *sobek.Runtime, obj *sobek.Object, name string, def time.Duration) time.Duration {
	v := obj.Get(name)
	if !isSet(v) {
		return def
	}
	return toDuration(rt, v, name)
}

// durationArg is an optional positional timeout argument.
func durationArg(rt *sobek.Runtime, v sobek.Value, def time.Duration) time.Duration {
	if !isSet(v) {
		return def
	}
	return toDuration(rt, v, "timeout")
}
