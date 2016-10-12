package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

type node string
type edges []node
type nodeSet map[node]struct{}
type graph map[node]edges

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

type depType uint8

const (
	depBefore depType = iota
	depAfter
)

type dep struct {
	rel    depType
	target string
}

type depGraph interface {
	addDep(name string, d *dep)
	addFree(name string)
	next() node
}

type hashDepGraph struct {
	n, f nodeSet
	g, h graph
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

func (t *hashDepGraph) addDep(name string, d *dep) {
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

var rDepLine = regexp.MustCompile(`^\s*#\s*(?:(before)|(after)):\s+(.+?)\s*$`)

func extractDepsFrom(r io.Reader, eachDep func(dep)) error {
	return extractHeadMatchesFrom(r, rDepLine, func(match []string) {
		d := dep{target: match[3]}
		if len(match[1]) != 0 {
			d.rel = depBefore
		} else if len(match[2]) != 0 {
			d.rel = depAfter
		}
		eachDep(d)
	})
}

func extractHeadMatchesFrom(r io.Reader, re *regexp.Regexp, each func([]string)) error {
	matching := false
	scanner := bufio.NewScanner(r)
	return matchEach(scanner, re, func(match []string) bool {
		if match != nil {
			matching = true
			each(match)
			return false
		} else if matching {
			return true
		} else {
			return false
		}
	})
}

func matchEach(scanner *bufio.Scanner, re *regexp.Regexp, each func([]string) bool) error {
	for scanner.Scan() {
		line := scanner.Text()
		match := re.FindStringSubmatch(line)
		if each(match) {
			break
		}
	}
	return scanner.Err()
}

func extractDeps(T depGraph, root string) (err error) {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// TODO: could support node identity declared inside the file rather
		// than derived (only) from file path
		name := filepath.Base(path)

		if name[0] == '.' {
			return nil
		}

		f, err := os.Open(path)
		any := false
		if err == nil {
			err = extractDepsFrom(f, func(d dep) {
				T.addDep(name, &d)
				any = true
			})
			if cerr := f.Close(); cerr != nil {
				err = cerr
			}
		}
		if !any {
			T.addFree(name)
		}

		return err
	})
}

func main() {
	flag.Parse()

	var err error
	var root string
	if narg := flag.NArg(); narg == 0 {
		root = "."
	} else if narg == 1 {
		root = flag.Arg(0)
	} else {
		log.Fatal("only want one arg")
	}

	if root, err = filepath.Abs(root); err != nil {
		log.Fatal(err)
	}

	T := newHashDepGraph()
	if err = extractDeps(T, root); err != nil {
		log.Fatal(err)
	}

	for n := T.next(); len(n) != 0; n = T.next() {
		p := filepath.Join(root, string(n))
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, p)
	}

	if len(T.g) > 0 {
		fmt.Println(T.g)
		log.Fatal("dependency cycle detected")
	}
}
