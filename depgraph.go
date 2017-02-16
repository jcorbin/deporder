package main

type depGraph interface {
	add(name string, ds ...dep)
	next() node
}

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
	target string
}

type hashDepGraph struct {
	n, f nodeSet
	g, h graph
}

func newHashDepGraph() *hashDepGraph {
	return &hashDepGraph{
		n: make(nodeSet),
		f: make(nodeSet),
		g: make(graph),
		h: make(graph),
	}
}

func (t *hashDepGraph) next() (n node) {
	if len(t.n) > 0 {
		n = t.n.min()
		t.removeNode(n)
		return
	}
	n = t.f.min()
	delete(t.f, n)
	return
}

func (t *hashDepGraph) add(name string, ds ...dep) {
	n := node(name)
	if len(ds) == 0 {
		if _, inG := t.g[n]; inG {
			return
		}
		if _, inH := t.h[n]; inH {
			return
		}
		t.f[n] = struct{}{}
		return
	}

	for _, d := range ds {
		switch d.rel {
		case depBefore:
			t.addEdge(n, node(d.target))
		case depAfter:
			t.addEdge(node(d.target), n)
		}
	}
}

func (t *hashDepGraph) addEdge(a, b node) {
	delete(t.f, a)
	delete(t.f, b)
	t.g.addEdge(a, b)
	t.h.addEdge(b, a)
	delete(t.n, b)
	if _, ok := t.h[a]; !ok {
		t.n[a] = struct{}{}
	} else {
		delete(t.n, a)
	}
}

func (t *hashDepGraph) removeNode(a node) {
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
