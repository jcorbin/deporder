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
type edges []node
type nodeSet map[node]struct{}
type graph map[node]edges

type dep struct {
	rel    depType
	target string
}

type namedDep struct {
	name string
	dep
}

type depGraph interface {
	addDep(name string, d dep)
	addFree(name string)
	next() node
}

type hashDepGraph struct {
	n, f nodeSet
	g, h graph
}

func (g graph) addEdge(a, b node) {
	ga, ok := g[a]
	if !ok {
		ga = make(edges, 1)
		ga[0] = b
	} else {
		ga = append(ga, b)
	}
	g[a] = ga
}

func newHashDepGraph() *hashDepGraph {
	T := new(hashDepGraph)
	T.n = make(nodeSet)
	T.f = make(nodeSet)
	T.g = make(graph)
	T.h = make(graph)
	return T
}

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

func (t *hashDepGraph) addDep(name string, d dep) {
	switch d.rel {
	case depBefore:
		t.addEdge(node(name), node(d.target))
	case depAfter:
		t.addEdge(node(d.target), node(name))
	}
}

func (t *hashDepGraph) addFree(name string) {
	n := node(name)
	if _, inG := t.g[n]; inG {
		return
	}
	if _, inH := t.h[n]; inH {
		return
	}
	t.f[n] = struct{}{}
}

func (t *hashDepGraph) addEdge(a, b node) {
	if _, inF := t.f[a]; inF {
		delete(t.f, a)
	}
	if _, inF := t.f[b]; inF {
		delete(t.f, b)
	}
	t.g.addEdge(a, b)
	t.h.addEdge(b, a)
	delete(t.n, b)
	if _, ok := t.h[a]; !ok {
		t.n[a] = struct{}{}
	} else if _, inN := t.n[a]; inN {
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
