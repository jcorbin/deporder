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

	type namedDep struct {
		name string
		dep
	}

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
