// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package graph

import (
	"errors"
	"time"

	"github.com/OWASP/Amass/v3/graphdb"
	"github.com/OWASP/Amass/v3/stringset"
	"golang.org/x/net/publicsuffix"
)

// InsertEvent create an event node in the graph that represents a discovery task.
func (g *Graph) InsertEvent(eventID string) (graphdb.Node, error) {
	// Check if there is an existing start time for this event.
	// If not, then create the node and add the start time/date
	var finish string

	g.eventFinishLock.Lock()
	defer g.eventFinishLock.Unlock()

	eventNode, err := g.db.ReadNode(eventID, "event")
	if err != nil {
		// Create a node to represent the event
		eventNode, err = g.db.InsertNode(eventID, "event")
		if err != nil {
			return eventNode, err
		}

		g.db.InsertProperty(eventNode, "start", time.Now().Format(time.RFC3339))
		if err != nil {
			return eventNode, err
		}
	}

	var ok bool
	curTime := time.Now()
	delta := 5 * time.Second
	var finishTime time.Time

	finish, ok = g.eventFinishes[eventID]
	if ok {
		finishTime, _ = time.Parse(time.RFC3339, finish)
	}

	// Remove an existing 'finish' property and enter a new one every 5 seconds
	if ok && (curTime.Sub(finishTime) > delta) {
		g.db.DeleteProperty(eventNode, "finish", finish)
	}

	if !ok || (curTime.Sub(finishTime) > delta) {
		finish = curTime.Format(time.RFC3339)

		// Update the finish property with the current time/date
		g.db.InsertProperty(eventNode, "finish", finish)
		if err != nil {
			return eventNode, err
		}

		g.eventFinishes[eventID] = finish
	}

	return eventNode, nil
}

// AddNodeToEvent creates associations between a node in the graph, a data source and a discovery task.
func (g *Graph) AddNodeToEvent(node graphdb.Node, source, tag, eventID string) error {
	if source == "" || tag == "" || eventID == "" {
		return errors.New("Graph: AddNodeToEvent: Invalid arguments provided")
	}

	eventNode, err := g.InsertEvent(eventID)
	if err != nil {
		return err
	}

	sourceNode, err := g.InsertSource(source, tag)
	if err != nil {
		return err
	}

	sourceEdge := &graphdb.Edge{
		Predicate: "used",
		From:      eventNode,
		To:        sourceNode,
	}
	if err := g.InsertEdge(sourceEdge); err != nil {
		return err
	}

	eventEdge := &graphdb.Edge{
		Predicate: source,
		From:      eventNode,
		To:        node,
	}

	return g.InsertEdge(eventEdge)
}

// InEventScope checks if the Node parameter is within scope of the Event identified by the uuid parameter.
func (g *Graph) InEventScope(node graphdb.Node, uuid string, predicates ...string) bool {
	edges, err := g.db.ReadInEdges(node, predicates...)
	if err != nil {
		return false
	}

	for _, edge := range edges {
		if g.db.NodeToID(edge.From) == uuid {
			return true
		}
	}

	return false
}

// EventList returns a list of event UUIDs found in the graph.
func (g *Graph) EventList() []string {
	nodes, err := g.AllNodesOfType("event")
	if err != nil {
		return nil
	}

	ids := stringset.New()
	for _, node := range nodes {
		ids.Insert(g.db.NodeToID(node))
	}

	return ids.Slice()
}

// EventFQDNs returns the domains that were involved in the event.
func (g *Graph) EventFQDNs(uuid string) []string {
	names, err := g.AllNodesOfType("fqdn", uuid)
	if err != nil {
		return nil
	}

	set := stringset.New()
	for _, name := range names {
		if n := g.db.NodeToID(name); n != "" {
			set.Insert(n)
		}
	}

	return set.Slice()
}

// EventDomains returns the domains that were involved in the event.
func (g *Graph) EventDomains(uuid string) []string {
	event, err := g.db.ReadNode(uuid, "event")
	if err != nil {
		return nil
	}

	edges, err := g.db.ReadOutEdges(event, "domain")
	if err != nil {
		return nil
	}

	domains := stringset.New()
	for _, edge := range edges {
		if d := g.db.NodeToID(edge.To); d != "" {
			domains.Insert(d)
		}
	}

	return domains.Slice()
}

// EventSubdomains returns the subdomains discovered during the event(s).
func (g *Graph) EventSubdomains(events ...string) []string {
	nodes, err := g.AllNodesOfType("fqdn", events...)
	if err != nil {
		return nil
	}

	var names []string
	for _, n := range nodes {
		d := g.db.NodeToID(n)
		etld, err := publicsuffix.EffectiveTLDPlusOne(d)
		if err != nil || etld == d {
			continue
		}

		names = append(names, g.db.NodeToID(n))
	}

	return names
}

// EventDateRange returns the date range associated with the provided event UUID.
func (g *Graph) EventDateRange(uuid string) (time.Time, time.Time) {
	var start, finish time.Time

	if event, err := g.db.ReadNode(uuid, "event"); err == nil {
		if properties, err := g.db.ReadProperties(event, "start", "finish"); err == nil {
			for _, p := range properties {
				if p.Predicate == "start" {
					start, _ = time.Parse(time.RFC3339, p.Value)
				} else {
					finish, _ = time.Parse(time.RFC3339, p.Value)
				}
			}
		}
	}

	return start, finish
}
