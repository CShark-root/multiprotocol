package compile

import (
	"strconv"
	"strings"

	"panel/internal/panel/store"
)

// XHTTP settings are split by who reads them. Both sides must agree on the
// "shape" (padding, placements, method); some knobs exist only on the server
// (buffering, SSE header) and some only on the client (xmux, post pacing).
// Field names follow Xray-core infra/conf SplitHTTPConfig.

// XHTTPServer is the xhttpSettings of the server inbound.
func XHTTPServer(x *store.XHTTPSettings) map[string]any {
	m := map[string]any{"path": x.Path, "mode": x.Mode}
	shape(m, x)
	if x.NoSSEHeader {
		m["noSSEHeader"] = true
	}
	if x.MaxBufferedPosts > 0 {
		m["scMaxBufferedPosts"] = x.MaxBufferedPosts
	}
	setRange(m, "scStreamUpServerSecs", x.StreamUpServerSecs)
	setRange(m, "scMaxEachPostBytes", x.MaxEachPostBytes)
	return m
}

// XHTTPClientExtra is what goes into a share link's "extra" parameter:
// everything the client must mirror plus client-only tuning. It returns nil
// when nothing differs from the defaults.
func XHTTPClientExtra(x *store.XHTTPSettings) map[string]any {
	m := map[string]any{}
	shape(m, x)
	if x.NoGRPCHeader {
		m["noGRPCHeader"] = true
	}
	setRange(m, "scMaxEachPostBytes", x.MaxEachPostBytes)
	setRange(m, "scMinPostsIntervalMs", x.MinPostsIntervalMs)
	if xm := x.Xmux; xm != nil {
		mux := map[string]any{}
		setRange(mux, "maxConcurrency", xm.MaxConcurrency)
		setRange(mux, "maxConnections", xm.MaxConnections)
		setRange(mux, "cMaxReuseTimes", xm.CMaxReuseTimes)
		setRange(mux, "hMaxRequestTimes", xm.HMaxRequestTimes)
		setRange(mux, "hMaxReusableSecs", xm.HMaxReusableSecs)
		if xm.HKeepAlivePeriod > 0 {
			mux["hKeepAlivePeriod"] = xm.HKeepAlivePeriod
		}
		if len(mux) > 0 {
			m["xmux"] = mux
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// shape writes the settings both sides must share.
func shape(m map[string]any, x *store.XHTTPSettings) {
	if x.Host != "" {
		m["host"] = x.Host
	}
	if len(x.Headers) > 0 {
		m["headers"] = x.Headers
	}
	setRange(m, "xPaddingBytes", x.PaddingBytes)
	if x.PaddingObfsMode {
		m["xPaddingObfsMode"] = true
	}
	setStr(m, "xPaddingPlacement", x.PaddingPlacement)
	setStr(m, "xPaddingMethod", x.PaddingMethod)
	setStr(m, "uplinkHTTPMethod", x.UplinkHTTPMethod)
	setStr(m, "sessionIDPlacement", x.SessionPlacement)
	setStr(m, "seqPlacement", x.SeqPlacement)
	setStr(m, "uplinkDataPlacement", x.UplinkDataPlacement)
}

func setStr(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

// setRange writes "N" as a number and "N-M" as a string, the two forms
// Xray's Int32Range accepts.
func setRange(m map[string]any, k, v string) {
	if v == "" {
		return
	}
	if !strings.Contains(v, "-") {
		if n, err := strconv.Atoi(v); err == nil {
			m[k] = n
			return
		}
	}
	m[k] = v
}
