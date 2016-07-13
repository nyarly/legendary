package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
	"time"

	"github.com/docopt/docopt-go"
	"golang.org/x/tools/cover"
)

//go:generate inlinefiles --package main . template.go
//go:generate inlinefiles --vfs=Templates --package main . vfs_template.go

const (
	docstring = `Parse go coverage profiles into vim-legend files
Usage: legendary [--project-root=<dir>] [--coverage-root=<dir>] <out_path> <coverage_path>...

Options:
	--coverage-root=<dir> Treat the coverage files as refering to files rooted at <dir> (Default: $GOPATH/src)
	--project-root=<dir>  Emit the vim-legend configuration rooted at <dir> (Default: $PWD)
`
)

type context struct {
	Now     int64
	Results map[string]*result
}

type result struct {
	counts  map[int]int
	Hits    []int
	Misses  []int
	Ignored []int
}

func main() {
	parsed, err := docopt.Parse(docstring, nil, true, "", false)
	if err != nil {
		log.Fatal(err)
	}

	sourceFiles := parsed[`<coverage_path>`].([]string)
	targetPath := parsed[`<out_path>`].(string)

	coverageRoot := filepath.Join(os.Getenv("GOPATH"), "src")
	if gcr, ok := parsed[`--coverage-root`]; ok && gcr != nil {
		coverageRoot = gcr.(string)
	}

	var projRoot string
	if gpr, ok := parsed[`--project-root`]; ok && gpr != nil {
		projRoot = gpr.(string)
	} else {
		projRoot, err = os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
	}

	var ctx context
	ctx.Results = make(map[string]*result)

	for _, fp := range sourceFiles {
		ps, err := cover.ParseProfiles(fp)
		if err != nil {
			log.Print(err)
			continue
		}

		for _, p := range ps {
			an := filepath.Join(coverageRoot, p.FileName)
			rn, err := filepath.Rel(projRoot, an)
			if err != nil {
				log.Print(err)
				continue
			}

			r, ok := ctx.Results[rn]
			if !ok {
				r = &result{counts: make(map[int]int)}
				ctx.Results[rn] = r
			}
			for _, pb := range p.Blocks {
				for ln := pb.StartLine; ln <= pb.EndLine; ln++ {
					r.counts[ln] += pb.Count
				}
			}
		}
	}

	for f, r := range ctx.Results {
		file, err := os.Open(filepath.Join(projRoot, f))
		if err != nil {
			log.Print(err)
			delete(ctx.Results, f)
			continue
		}

		lines, err := lineCounter(file)
		if err != nil {
			log.Print(err)
			delete(ctx.Results, f)
			continue
		}

		for ln := 0; ln < lines; ln++ {
			c, ok := r.counts[ln]
			switch {
			default:
				r.Misses = append(r.Misses, ln)
			case !ok:
				r.Ignored = append(r.Ignored, ln)
			case c > 0:
				r.Hits = append(r.Hits, ln)
			}
		}
	}

	ctx.Now = time.Now().Unix()
	tmpl := getTemplate("coverage.tmpl")

	out := &bytes.Buffer{}
	file, err := os.Create(targetPath)
	if err != nil {
		log.Fatal(err)
	}

	tmpl.Execute(out, ctx)

	file.Write(out.Bytes())
}

func getTemplate(n string) *template.Template {
	tmplFile, err := Templates.Open(n)
	if err != nil {
		log.Fatal(err)
	}
	tmplB := &bytes.Buffer{}
	_, err = tmplB.ReadFrom(tmplFile)
	if err != nil {
		log.Fatal(err)
	}
	return template.Must(template.New("root").Parse(tmplB.String()))
}

func lineCounter(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

type escaper struct {
	r     io.Reader
	old   []byte
	debug bool
}

var doublesRE = regexp.MustCompile(`"`)
var newlsRE = regexp.MustCompile("(?m)\n")

func (e *escaper) Read(p []byte) (n int, err error) {
	new := make([]byte, len(p)-len(e.old))
	c, err := e.r.Read(new)
	new = append(e.old, new[0:c]...)

	i, n := 0, 0
	for ; i < len(new) && n < len(p); i, n = i+1, n+1 {
		switch new[i] {
		default:
			p[n] = new[i]
		case '"', '\\':
			p[n] = '\\'
			n++
			p[n] = new[i]
		case '\n':
			p[n] = '\\'
			n++
			p[n] = 'n'
		}
	}
	if len(p) < i {
		e.old = new[len(new)-(len(p)-i):]
	} else {
		e.old = new[0:0]
	}

	if e.debug {
		log.Print(i, "/", n, "\n", len(e.old), ":", string(e.old), "\n", len(p), ":", string(p), "\n\n**************************\n\n")
	}

	return
}

func newEscaper(r io.Reader) *escaper {
	return &escaper{r, make([]byte, 0), false}
}
