package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OtlpMillService is the service.name of the spans of the mill's own phases;
// the phone's phases carry the app's name.
const OtlpMillService = "mw-mill"

// otlpScope is the instrumentation scope every span is written under.
const otlpScope = "millwright"

// The OTLP/JSON of the OpenTelemetry specification, the part mw writes: a
// document of resourceSpans, each holding scopeSpans, each holding spans.
// 64-bit integers are strings, trace and span ids are lowercase hex.
type OtlpDocument struct {
	ResourceSpans []OtlpResourceSpans `json:"resourceSpans"`
}

type OtlpResourceSpans struct {
	Resource   OtlpResource     `json:"resource"`
	ScopeSpans []OtlpScopeSpans `json:"scopeSpans"`
}

type OtlpResource struct {
	Attributes []OtlpAttribute `json:"attributes"`
}

type OtlpScopeSpans struct {
	Scope OtlpScope  `json:"scope"`
	Spans []OtlpSpan `json:"spans"`
}

type OtlpScope struct {
	Name string `json:"name"`
}

type OtlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	Kind              int             `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []OtlpAttribute `json:"attributes,omitempty"`
}

type OtlpAttribute struct {
	Key   string    `json:"key"`
	Value OtlpValue `json:"value"`
}

// OtlpValue holds one of the value kinds mw writes: a string or a double.
type OtlpValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

func otlpString(key, value string) OtlpAttribute {
	return OtlpAttribute{Key: key, Value: OtlpValue{StringValue: &value}}
}

func otlpDouble(key string, value float64) OtlpAttribute {
	return OtlpAttribute{Key: key, Value: OtlpValue{DoubleValue: &value}}
}

// otlpSpanKindInternal is SPAN_KIND_INTERNAL.
const otlpSpanKindInternal = 1

// OtlpTraceID is the trace id of a grist: the first 16 bytes of its txid's
// hex (the txid after its "direct:" or other prefix). A txid with fewer than
// 16 bytes of hex has the first 16 bytes of the SHA-256 of the whole txid
// instead, so the id is always 32 hex digits, never all zeros, and the same
// every time.
func OtlpTraceID(txid string) string {
	_, tail, found := strings.Cut(txid, ":")
	if !found {
		tail = txid
	}
	if len(tail) >= 32 {
		if _, err := hex.DecodeString(strings.ToLower(tail[:32])); err == nil {
			if id := strings.ToLower(tail[:32]); strings.Trim(id, "0") != "" {
				return id
			}
		}
	}
	sum := sha256.Sum256([]byte(txid))
	return hex.EncodeToString(sum[:16])
}

// otlpSpanID is the id of one span of a trace: 8 bytes of the SHA-256 of the
// trace id and the span's name, so a run again gives the same ids.
func otlpSpanID(traceID, name string) string {
	sum := sha256.Sum256([]byte(traceID + "/" + name))
	return hex.EncodeToString(sum[:8])
}

func otlpNano(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

// otlpSeconds is a phase's seconds as a duration, or false when the phase is
// not known.
func otlpSeconds(s *float64) (time.Duration, bool) {
	if s == nil {
		return 0, false
	}
	return time.Duration(*s * float64(time.Second)), true
}

// otlpPhase is one span of a grist's trace before it is written.
type otlpPhase struct {
	name       string
	service    string
	start, end time.Time
	attrs      []OtlpAttribute
}

// otlpPhases lays a grind line's known phases on a timeline. The line's time
// is when the answer was delivered (the record is written then); the grist was
// sent total_s before it, and the mill's phases run forward from there in
// order, the deliver phase ending at the line's time. Without a total the
// phases run back from the line's time instead. The app's phone phase ends at
// the moment the grist was sent, and so does its network phase, which is the
// sum of two round trips (the request, and the poll that saw the answer) and
// so has no single place; it is put where the request was. Seconds between
// phases (the grind read, photos opened) are a gap, not a span.
func otlpPhases(line GrindLine, client *GristClientTiming) (phases []otlpPhase) {
	var sent time.Time
	end := line.Time
	type mill struct {
		name    string
		seconds *float64
	}
	var before []mill
	for _, m := range []mill{
		{"wake", line.WakeSeconds}, {"pickup", line.PickupSeconds}, {"slot", line.SlotSeconds},
		{"startup", line.StartupSeconds}, {"model", line.ModelSeconds},
	} {
		if _, ok := otlpSeconds(m.seconds); ok {
			before = append(before, m)
		}
	}
	deliver, haveDeliver := otlpSeconds(line.DeliverSeconds)
	chainEnd := end
	if haveDeliver {
		chainEnd = end.Add(-deliver)
	}
	var millPhases []otlpPhase
	if total, ok := otlpSeconds(line.TotalSeconds); ok {
		at := end.Add(-total)
		sent = at
		for _, m := range before {
			d, _ := otlpSeconds(m.seconds)
			millPhases = append(millPhases, otlpPhase{name: m.name, service: OtlpMillService, start: at, end: at.Add(d)})
			at = at.Add(d)
		}
	} else {
		at := chainEnd
		for i := len(before) - 1; i >= 0; i-- {
			d, _ := otlpSeconds(before[i].seconds)
			millPhases = append([]otlpPhase{{name: before[i].name, service: OtlpMillService, start: at.Add(-d), end: at}}, millPhases...)
			at = at.Add(-d)
		}
		sent = at
		if len(before) == 0 {
			sent = chainEnd.Add(-time.Duration(line.Seconds * float64(time.Second)))
		}
	}
	if haveDeliver {
		millPhases = append(millPhases, otlpPhase{name: "deliver", service: OtlpMillService, start: chainEnd, end: end})
	}
	if client != nil {
		service := line.App
		if service == "" {
			service = "unknown-app"
		}
		if d, ok := otlpSeconds(msSeconds(client.TapToSent)); ok {
			phases = append(phases, otlpPhase{name: "phone", service: service, start: sent.Add(-d), end: sent})
		}
		if d, ok := otlpSeconds(msSeconds(client.Network)); ok {
			phases = append(phases, otlpPhase{name: "network", service: service, start: sent.Add(-d), end: sent})
		}
	}
	return append(phases, millPhases...)
}

// Otlp is the same record as Run reads, written as OpenTelemetry traces in
// OTLP/JSON: one trace for each grind line, its id from the grist's txid, a
// root span "grist <kind>" and a child span for each phase the record knows.
// It writes one line of JSON to Out and nothing else. since, app and kind
// keep lines as Run does. The document it wrote is returned.
func (g GristTiming) Otlp(ctx context.Context, since time.Time, app, kind string) (OtlpDocument, error) {
	var doc OtlpDocument
	if g.State == nil {
		return doc, fmt.Errorf("mw grist timing: nowhere the mill keeps its record")
	}
	lines, err := g.State.Lines(ctx)
	if err != nil {
		return doc, err
	}
	reported, err := g.State.ClientTimings(ctx)
	if err != nil {
		return doc, err
	}
	clients := map[string]GristClientTiming{}
	for _, c := range reported {
		if _, ok := clients[c.Txid]; !ok {
			clients[c.Txid] = c
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Time.Before(lines[j].Time) })

	byService := map[string][]OtlpSpan{}
	var order []string
	add := func(service string, span OtlpSpan) {
		if _, ok := byService[service]; !ok {
			order = append(order, service)
		}
		byService[service] = append(byService[service], span)
	}
	for _, line := range lines {
		if line.Time.Before(since) || (app != "" && line.App != app) || (kind != "" && line.Kind != kind) {
			continue
		}
		var client *GristClientTiming
		if c, ok := clients[line.Txid]; ok {
			client = &c
		}
		traceID := OtlpTraceID(line.Txid)
		phases := otlpPhases(line, client)

		rootStart, rootEnd := line.Time.Add(-time.Duration(line.Seconds*float64(time.Second))), line.Time
		for i, p := range phases {
			if i == 0 || p.start.Before(rootStart) {
				rootStart = p.start
			}
			if p.end.After(rootEnd) {
				rootEnd = p.end
			}
		}
		status := line.Status
		if IsGristReasonLimited(line.Reason) {
			status = "limited"
		}
		rootName := "grist " + line.Kind
		rootID := otlpSpanID(traceID, "root")
		rootAttrs := []OtlpAttribute{
			otlpString("mw.txid", line.Txid), otlpString("mw.app", line.App), otlpString("mw.kind", line.Kind),
			otlpString("mw.status", status), otlpDouble("mw.seconds", line.Seconds),
		}
		if client != nil {
			for _, a := range []struct {
				key string
				ms  *float64
			}{{"mw.delivered_to_seen_s", client.DeliveredToSeen}, {"mw.seen_to_shown_s", client.SeenToShown}} {
				if s := msSeconds(a.ms); s != nil {
					rootAttrs = append(rootAttrs, otlpDouble(a.key, *s))
				}
			}
		}
		add(OtlpMillService, OtlpSpan{
			TraceID: traceID, SpanID: rootID, Name: rootName, Kind: otlpSpanKindInternal,
			StartTimeUnixNano: otlpNano(rootStart), EndTimeUnixNano: otlpNano(rootEnd), Attributes: rootAttrs,
		})
		for _, p := range phases {
			add(p.service, OtlpSpan{
				TraceID: traceID, SpanID: otlpSpanID(traceID, p.name), ParentSpanID: rootID, Name: p.name, Kind: otlpSpanKindInternal,
				StartTimeUnixNano: otlpNano(p.start), EndTimeUnixNano: otlpNano(p.end), Attributes: p.attrs,
			})
		}
	}
	doc.ResourceSpans = []OtlpResourceSpans{}
	for _, service := range order {
		doc.ResourceSpans = append(doc.ResourceSpans, OtlpResourceSpans{
			Resource:   OtlpResource{Attributes: []OtlpAttribute{otlpString("service.name", service)}},
			ScopeSpans: []OtlpScopeSpans{{Scope: OtlpScope{Name: otlpScope}, Spans: byService[service]}},
		})
	}
	if g.Out != nil {
		body, err := json.Marshal(doc)
		if err != nil {
			return doc, err
		}
		if _, err := fmt.Fprintf(g.Out, "%s\n", body); err != nil {
			return doc, err
		}
	}
	return doc, nil
}
