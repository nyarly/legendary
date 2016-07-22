package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/docopt/docopt-go"
	"golang.org/x/tools/cover"
)

//go:generate inlinefiles --package main . template.go
//go:generate inlinefiles --vfs=Templates --package main . vfs_template.go

const (
	docstring = `Parse go coverage profiles into vim-legend files
Usage: legendary [options] <out_path> <coverage_path>...
       legendary [options] --hitlist [--limit=n] <coverage_path>...

Options:
	--coverage-root=<dir> Treat the coverage files as refering to files rooted at <dir> (Default: $GOPATH/src)
	--project-root=<dir>  Emit the vim-legend configuration rooted at <dir> (Default: $PWD)
	--hitlist             Don't produce vim-legend output - instead repoort on worst covered files
	--limit=<n>           Limit the number of files in the hitlist to <n>
`
)

type (
	context struct {
		Now     int64
		Results map[string]*result
	}

	result struct {
		filename string
		lines    int
		counts   map[int]int
		Hits     []int
		Misses   []int
		Ignored  []int
	}
	resultsByLines      []*result
	resultsByPercentage []*result
)

func (rz *result) MissCount() int {
	return len(rz.Misses)
}

func (rz *result) HitCount() int {
	return len(rz.Hits)
}

func (rz *result) MissFraction() float64 {
	hc, mc := rz.HitCount(), rz.MissCount()
	return float64(mc) / float64(hc+mc)
}

func (r resultsByPercentage) Len() int {
	return len(r)
}
func (r resultsByPercentage) Less(i, j int) bool {
	return r[i].MissFraction() < r[j].MissFraction()
}
func (r resultsByPercentage) Swap(i, j int) {
	n, m := r[i], r[j]
	r[j], r[i] = n, m
}

func (r resultsByLines) Len() int {
	return len(r)
}
func (r resultsByLines) Less(i, j int) bool {
	return r[i].MissCount() < r[j].MissCount()
}
func (r resultsByLines) Swap(i, j int) {
	n, m := r[i], r[j]
	r[j], r[i] = n, m
}

func main() {
	parsed, err := docopt.Parse(docstring, nil, true, "", false)
	if err != nil {
		log.Fatal(err)
	}

	sourceFiles := parsed[`<coverage_path>`].([]string)
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

	ctx := collectCoverageContext(coverageRoot, projRoot, sourceFiles)

	if parsed[`--hitlist`].(bool) {
		var rs resultsByLines
		for _, v := range ctx.Results {
			rs = append(rs, v)
		}
		sort.Sort(sort.Reverse(rs))

		var ps resultsByPercentage
		for _, v := range ctx.Results {
			ps = append(ps, v)
		}
		sort.Sort(sort.Reverse(ps))

		var rps [][2]*result
		for i := range rs {
			rps = append(rps, [2]*result{rs[i], ps[i]})
		}

		len := len(rps)
		if lim := parsed["--limit"]; lim != nil {
			l, err := strconv.ParseInt(lim.(string), 10, 0)
			if err == nil {
				len = int(l)
			}
		}

		tabs := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintf(tabs, "File\tMissed Lines\tFile\tPercent missed\n")
		for i := 0; i < len; i++ {
			fmt.Fprintf(tabs, "%s\t%d\t%s\t%.2f\n",
				rps[i][0].filename, rps[i][0].MissCount(), rps[i][1].filename, rps[i][1].MissFraction()*100)
		}
		tabs.Flush()
		return
	}
	targetPath := parsed[`<out_path>`].(string)
	tmpl := getTemplate("coverage.tmpl")

	out := &bytes.Buffer{}
	file, err := os.Create(targetPath)
	if err != nil {
		log.Fatal(err)
	}

	tmpl.Execute(out, ctx)

	file.Write(out.Bytes())
}

func missOutput(rs []*result, header string, fn func(*result) string) {
}

func injestCoverageFile(ctx *context, coverageRoot, projRoot, fp string) {
	ps, err := cover.ParseProfiles(fp)
	if err != nil {
		log.Print(err)
		return
	}

	for _, p := range ps {
		an := filepath.Join(coverageRoot, p.FileName)
		rn, err := filepath.Rel(projRoot, an)
		if err != nil {
			log.Print(err)
			return
		}

		r, ok := ctx.Results[rn]
		if !ok {
			r = &result{filename: rn, counts: make(map[int]int)}
			ctx.Results[rn] = r
		}
		for _, pb := range p.Blocks {
			for ln := pb.StartLine; ln <= pb.EndLine; ln++ {
				r.counts[ln] += pb.Count
			}
		}
	}
	return
}

func buildFileCoverage(ctx *context, projRoot, f string, r *result) {
	file, err := os.Open(filepath.Join(projRoot, f))
	if err != nil {
		log.Print(err)
		delete(ctx.Results, f)
		return
	}

	lines, err := lineCounter(file)
	if err != nil {
		log.Print(err)
		delete(ctx.Results, f)
		return
	}
	r.lines = lines

	for ln := 0; ln < r.lines; ln++ {
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
	return
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

// XXX
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
func collectCoverageContext(coverageRoot string, projRoot string, sourceFiles []string) context {
	var ctx context
	ctx.Results = make(map[string]*result)

	for _, fp := range sourceFiles {
		injestCoverageFile(&ctx, coverageRoot, projRoot, fp)
	}

	for f, r := range ctx.Results {
		buildFileCoverage(&ctx, projRoot, f, r)
	}

	ctx.Now = time.Now().Unix()
	return ctx
}
