package index

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// Triggers edge props.
const (
	PropEventName = "event" // comma-separated event names the edge carries
	PropEventVia  = "via"   // comma-separated send kinds: send, step, outbox
)

// resolveEvents links event senders to the functions their events trigger:
// a triggers edge from each declaration with an event_sends record to every
// declaration with an event_triggers record for the same event name. A
// function that sends its own trigger event (a requeue) gets no self edge.
func (idx *Indexer) resolveEvents(log *slog.Logger) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	receivers := map[string][]graph.NodeID{}
	var senders []graph.Node
	err := idx.store.ForEachNode(func(n graph.Node) bool {
		for _, rec := range parse.DecodeRecords(n.Props[parse.PropEventTriggers]) {
			if len(rec) > 0 && rec[0] != "" {
				receivers[rec[0]] = append(receivers[rec[0]], n.ID)
			}
		}
		if n.Props[parse.PropEventSends] != "" {
			senders = append(senders, n)
		}
		return true
	})
	if err != nil {
		return err
	}
	for name, ids := range receivers {
		receivers[name] = dedupIDs(ids)
	}
	sort.Slice(senders, func(i, j int) bool { return senders[i].ID < senders[j].ID })

	links := 0
	for _, s := range senders {
		type link struct{ events, via map[string]bool }
		desired := map[graph.NodeID]*link{}
		for _, rec := range parse.DecodeRecords(s.Props[parse.PropEventSends]) {
			if len(rec) < 2 {
				continue
			}
			for _, to := range receivers[rec[0]] {
				if to == s.ID {
					continue
				}
				l := desired[to]
				if l == nil {
					l = &link{events: map[string]bool{}, via: map[string]bool{}}
					desired[to] = l
				}
				l.events[rec[0]] = true
				l.via[rec[1]] = true
			}
		}

		existing, err := idx.store.OutEdges(s.ID, parse.EdgeTriggers)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if desired[e.To] == nil {
				if err := idx.store.DeleteEdge(s.ID, e.To, parse.EdgeTriggers); err != nil {
					return err
				}
			}
		}
		targets := make([]graph.NodeID, 0, len(desired))
		for id := range desired {
			targets = append(targets, id)
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
		for _, to := range targets {
			l := desired[to]
			if err := idx.store.PutEdge(graph.Edge{
				From: s.ID,
				To:   to,
				Type: parse.EdgeTriggers,
				Props: map[string]string{
					parse.PropProvenance: parse.ProvenanceInferred,
					PropEventName:        joinSorted(l.events),
					PropEventVia:         joinSorted(l.via),
				},
			}); err != nil {
				return err
			}
			links++
		}
	}
	log.Info("index resolved events", "receivers", len(receivers), "senders", len(senders), "links", links)
	return nil
}

func dedupIDs(in []graph.NodeID) []graph.NodeID {
	sort.Slice(in, func(i, j int) bool { return in[i] < in[j] })
	out := in[:0]
	for i, id := range in {
		if i == 0 || id != in[i-1] {
			out = append(out, id)
		}
	}
	return out
}

func joinSorted(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
