package parse

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Event records are list props written at parse time and resolved at index
// time into triggers edges from the code that sends an event to the
// functions it triggers (see internal/index resolve_events.go). Records use
// EncodeRecord, like the HTTP records.
const (
	// PropEventSends (function, method or module): event|via|line. The
	// declaration sends the named event. via is "send" (inngest.send),
	// "step" (step.sendEvent) or "outbox" (an outbox insert carrying the
	// event name, dispatched later).
	PropEventSends = "event_sends"
	// PropEventTriggers (function or module): event|line. The declaration is
	// a function that runs when the named event is sent
	// (inngest.createFunction with an event trigger).
	PropEventTriggers = "event_triggers"
)

// Event send kinds (the via field of a PropEventSends record).
const (
	EventViaSend   = "send"
	EventViaStep   = "step"
	EventViaOutbox = "outbox"
)

// maxEventPayloadDepth bounds the walk into a send's payload argument.
const maxEventPayloadDepth = 8

// extractEventsJS records Inngest event senders and receivers in a JS or TS
// file. A sender is `<inngest client>.send(payload)`, `step.sendEvent(id,
// payload)`, or a call whose name mentions an outbox and passes an object
// with an `eventName`; the payload's `name` must be a static string. A
// receiver is `<x>.createFunction(config, …)` whose config has
// `triggers: [{ event }]` or `trigger: { event }`, or the v2 form
// `createFunction(config, { event }, handler)`.
func (x *jsHTTP) extractEventsJS(root *tree_sitter.Node) {
	walkNamed(root, func(n *tree_sitter.Node) {
		if n.Kind() != "call_expression" {
			return
		}
		fn := field(n, "function")
		if fn != nil && fn.Kind() == "await_expression" && fn.NamedChildCount() > 0 {
			fn = fn.NamedChild(0)
		}
		if fn == nil {
			return
		}
		args := callArgs(n)
		if len(args) == 0 {
			return
		}
		line := lineOf(n)
		var recv, prop string
		switch fn.Kind() {
		case "member_expression":
			recv, prop = x.b.text(field(fn, "object")), x.b.text(field(fn, "property"))
		case "identifier":
			prop = x.b.text(fn)
		default:
			return
		}
		switch {
		case prop == "send" && strings.Contains(strings.ToLower(recv), "inngest"):
			x.eventSends(args[0], EventViaSend, []string{"name"}, line)
		case prop == "sendEvent" && recv != "":
			// step.sendEvent(id, payload); the payload is the last argument.
			x.eventSends(args[len(args)-1], EventViaStep, []string{"name"}, line)
		case strings.Contains(strings.ToLower(recv+"."+prop), "outbox"):
			for _, a := range args {
				x.eventSends(a, EventViaOutbox, []string{"eventName", "event_name"}, line)
			}
		case prop == "createFunction" && recv != "":
			x.eventTriggers(args, line)
		}
	})
}

// eventSends records each static event name a payload sends: the `keys`
// properties of the payload object, of each element of an array of them, and
// of objects built by callbacks (`ids.map((id) => ({ name: 'x/y', … }))`).
// A property's value is not entered, so `data: { name }` is not an event.
func (x *jsHTTP) eventSends(payload *tree_sitter.Node, via string, keys []string, line int) {
	owner := x.rec.ownerAt(line)
	seen := map[string]bool{}
	var walk func(n *tree_sitter.Node, depth int)
	walk = func(n *tree_sitter.Node, depth int) {
		if n == nil || depth > maxEventPayloadDepth {
			return
		}
		if n.Kind() == "pair" {
			key := propertyKey(x.b, field(n, "key"))
			for _, k := range keys {
				if key != k {
					continue
				}
				if name, ok := x.staticString(field(n, "value")); ok && name != "" && !seen[name] {
					seen[name] = true
					x.b.addListProp(owner, PropEventSends, EncodeRecord(name, via, strconv.Itoa(line)))
				}
			}
			return
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			walk(n.NamedChild(i), depth+1)
		}
	}
	walk(payload, 0)
}

// eventTriggers records the events a createFunction call runs on.
func (x *jsHTTP) eventTriggers(args []*tree_sitter.Node, line int) {
	owner := x.rec.ownerAt(line)
	record := func(obj *tree_sitter.Node) {
		obj = unwrapTSExpression(obj)
		if obj == nil || obj.Kind() != "object" {
			return
		}
		if v := objectProp(x.b, obj, "event"); v != nil {
			if name, ok := x.staticString(v); ok && name != "" {
				x.b.addListProp(owner, PropEventTriggers, EncodeRecord(name, strconv.Itoa(line)))
			}
		}
	}
	// v3: createFunction({ id, triggers: [{ event }] }, handler).
	if cfg := unwrapTSExpression(args[0]); cfg != nil && cfg.Kind() == "object" {
		for _, key := range []string{"triggers", "trigger"} {
			v := unwrapTSExpression(objectProp(x.b, cfg, key))
			if v == nil {
				continue
			}
			if v.Kind() == "array" {
				for i := uint(0); i < v.NamedChildCount(); i++ {
					record(v.NamedChild(i))
				}
			} else {
				record(v)
			}
		}
	}
	// v2: createFunction({ id }, { event }, handler).
	if len(args) >= 3 {
		record(args[1])
	}
}

// staticString evaluates a string literal, a template without
// substitutions or a same-file string constant, looking through `as const`.
func (x *jsHTTP) staticString(n *tree_sitter.Node) (string, bool) {
	n = unwrapTSExpression(n)
	if n == nil {
		return "", false
	}
	return x.eval(n, nil, 0).literal()
}

// objectProp is the value of the named property of an object literal.
func objectProp(b *builder, obj *tree_sitter.Node, name string) *tree_sitter.Node {
	for i := uint(0); i < obj.NamedChildCount(); i++ {
		c := obj.NamedChild(i)
		if c.Kind() == "pair" && propertyKey(b, field(c, "key")) == name {
			return field(c, "value")
		}
	}
	return nil
}

// propertyKey is an object key's name: `name`, `'name'` or `"name"`.
func propertyKey(b *builder, k *tree_sitter.Node) string {
	if k == nil {
		return ""
	}
	switch k.Kind() {
	case "property_identifier", "identifier":
		return b.text(k)
	case "string":
		return unquote(b.text(k))
	}
	return ""
}
