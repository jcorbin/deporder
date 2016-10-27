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
	"text/template"
	"time"

	"golang.org/x/sync/errgroup"
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

func extractDeps(T depGraph, root string) (time.Time, error) {
	const bufSize = 10
	var eg errgroup.Group

	deps := make(chan namedDep, bufSize)
	free := make(chan string, bufSize)
	mtimes := make(chan time.Time, bufSize)

	done := make(chan time.Time)

	go func(deps <-chan namedDep, free <-chan string, mtimes <-chan time.Time) {
		var mtime time.Time
		for deps != nil && free != nil && mtimes != nil {
			select {
			case nd, ok := <-deps:
				if !ok {
					deps = nil
					continue
				}
				T.addDep(nd.name, nd.dep)
			case name, ok := <-free:
				if !ok {
					free = nil
					continue
				}
				T.addFree(name)
			case mt, ok := <-mtimes:
				if !ok {
					mtimes = nil
					continue
				}
				if mt.After(mtime) {
					mtime = mt
				}
			}
		}
		done <- mtime
	}(deps, free, mtimes)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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

		eg.Go(func() error {
			st, err := os.Stat(path)
			if err != nil {
				return err
			}
			mtimes <- st.ModTime()

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			any := false
			err = extractDepsFrom(f, func(d dep) {
				deps <- namedDep{name, d}
				any = true
			})
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil && !any {
				free <- name
			}
			return err
		})

		return nil
	})

	if gerr := eg.Wait(); err == nil {
		err = gerr
	}
	close(deps)
	close(free)
	close(mtimes)
	mtime := <-done
	return mtime, err
}

var (
	out   = flag.String("out", "", "")
	timed = flag.Bool("timed", false, "")
)

var defaultTemplate = template.Must(template.New("defaultTemplate").Parse(`
{{ define "before" }}
# START {{ .Name }}
# from {{ .Path }}
{{ end }}

{{ define "after" }}
# END {{ .Name }}
{{ end }}
`))

var timedTemplate = template.Must(template.New("timedTemplate").Parse(`
{{ define "before" }}
# START {{ .Name }}
# from {{ .Path }}
{ echo -n {{ .Name }}; time (
{{ end }}

{{ define "after" }}
) }
# END {{ .Name }}
{{ end }}
`))

type depCompiler struct {
	tmpl     *template.Template
	notFirst bool
	w        io.Writer
	d        struct {
		Name, Path string
	}
}

func (dc depCompiler) compile(root string, T *hashDepGraph) error {
	for n := T.next(); len(n) != 0; n = T.next() {
		dc.d.Name = string(n)
		dc.d.Path = filepath.Join(root, dc.d.Name)
		f, err := os.Open(dc.d.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		err = dc.compDep(f)
		if err == nil {
			err = f.Close()
		}
		if err != nil {
			return err
		}
	}
	if len(T.g) > 0 {
		return fmt.Errorf("dependency cycle detected: %q", T.g)
	}
	return nil
}

func (dc depCompiler) compDep(r io.Reader) error {
	if dc.notFirst {
		if _, err := io.WriteString(dc.w, "\n"); err != nil {
			return err
		}
	} else {
		dc.notFirst = true
	}
	if err := dc.tmpl.ExecuteTemplate(dc.w, "before", dc.d); err != nil {
		return err
	}
	if _, err := io.Copy(dc.w, r); err != nil {
		return err
	}
	return dc.tmpl.ExecuteTemplate(dc.w, "after", dc.d)
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
	mtime, err := extractDeps(T, root)
	if err != nil {
		log.Fatal(err)
	}

	dc := depCompiler{
		tmpl: defaultTemplate,
		w:    os.Stdout,
	}

	if *out != "" {
		var f *os.File
		if err := func() error {
			st, err := os.Stat(*out)
			if err == nil {
				if mt := st.ModTime(); mt.After(mtime) {
					os.Exit(0)
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			f, err = os.Create(*out)
			return err
		}(); err != nil {
			log.Fatal(err)
		}

		defer func() {
			_ = f.Close()
		}()
		dc.w = f
	}

	if *timed {
		dc.tmpl = timedTemplate
	}

	if err := dc.compile(root, T); err != nil {
		log.Fatal(err)
	}
}
