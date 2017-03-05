package main

type depType uint8

const (
	depBefore depType = iota
	depAfter
)

func (dt depType) String() string {
	switch dt {
	case depBefore:
		return "before"
	case depAfter:
		return "after"
	default:
		return "InvalidDepType"
	}
}

type node string
type nodes []node

type nodeSet map[node]struct{}

func (ns nodeSet) min() (n node) {
	for in := range ns {
		if len(n) == 0 {
			n = in
		} else if in < n {
			n = in
		}
	}
	return
}

type graph map[node]nodes

func (g graph) addEdge(a, b node) {
	ga, ok := g[a]
	if !ok {
		ga = make(nodes, 1)
		ga[0] = b
	} else {
		ga = append(ga, b)
	}
	g[a] = ga
}

type dep struct {
	rel    depType
	target node
}

type depGraph struct {
	// the set of root dependency nodes
	n nodeSet
	// free nodes
	f nodeSet
	// graph of dependencies
	g graph
	// its co-graph (aka dependants)
	h graph
}

func newDepGraph() *depGraph {
	return &depGraph{
		n: make(nodeSet),
		f: make(nodeSet),
		g: make(graph),
		h: make(graph),
	}
}

// next removes and returns the lexicographically least root dependency or free
// node once all dependencies have been traversed.
func (t *depGraph) next() (n node) {
	if len(t.n) > 0 {
		n = t.n.min()
		t.remove(n)
		return
	}
	n = t.f.min()
	delete(t.f, n)
	return
}

// add adds a node with zero or more dependency relations to the graph.
func (t *depGraph) add(n node, ds ...dep) {
	// If we have no deps, and this is the first time that we've seen the node,
	// then add it to the free set.
	if len(ds) == 0 {
		_, inG := t.g[n]
		_, inH := t.h[n]
		if !inG && !inH {
			t.f[n] = struct{}{}
		}
		return
	}

	// Otherwise we have a node with some dependency relations: ensure un-free
	// it and add new edges.
	delete(t.f, n)
	for _, d := range ds {
		a, b := n, d.target
		delete(t.f, b)
		if d.rel == depAfter {
			a, b = b, a
		}
		t.g.addEdge(a, b)
		t.h.addEdge(b, a)
		delete(t.n, b)
		if _, ok := t.h[a]; !ok {
			// if a has no dependencies, then it's a root node (so far); put it
			// into n so that next will get to it
			t.n[a] = struct{}{}
		} else {
			// a has some dependencies, and we'll get to it after next has
			// processed all of a's dependencies.
			delete(t.n, a)
		}
	}
}

// remove removes a dependency from the graph, adding adds all dependants
// that have no more dependencies to the root node set.
func (t *depGraph) remove(a node) {
	ga := t.g[a]

	delete(t.f, a)
	delete(t.n, a)
	delete(t.g, a)

	for _, b := range ga {
		hb := t.h[b]
		for i, c := range hb {
			if a == c {
				hb[i] = hb[len(hb)-1]
				hb = hb[:len(hb)-1]
				break
			}
		}
		if len(hb) == 0 {
			delete(t.h, b)
			t.n[b] = struct{}{}
		} else {
			t.h[b] = hb
		}
	}
}
