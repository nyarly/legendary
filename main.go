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
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/nyarly/coerce"
	"golang.org/x/tools/cover"
)

//go:generate inlinefiles --vfs=Templates --package main . vfs_template.go

const (
	docstring = `Parse go coverage profiles into vim-legend files
Usage: legendary [options] <outpath> <sourcefiles>...
       legendary [options] --noout <sourcefiles>...

Options:
	--coverage=<dir>      Treat the coverage files as refering to files rooted at <dir> (Default: $GOPATH/src)
	--project=<dir>       Emit the vim-legend configuration rooted at <dir> (Default: $PWD)
	--hitlist             Don't produce vim-legend output - instead repoort on worst covered files
	--limit=<n>           Limit the number of files in the hitlist to <n>
	--noout               Don't record the coverage anywhere (use with hitlist)
`
)

type (
	options struct {
		coverage, project string
		hitlist           bool
		limit             uint
		outpath           string
		sourcefiles       []string
	}

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
	log.SetFlags(log.Flags() | log.Lshortfile)
	opts := parseOpts()

	ctx := collectCoverageContext(opts.coverage, opts.project, opts.sourcefiles)

	if opts.hitlist {
		printHitlist(ctx, int(opts.limit))
	}

	writeOut(ctx, opts)
}

func printHitlist(ctx context, limit int) {
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

	if limit == 0 {
		limit = len(rps)
	}

	tabs := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintf(tabs, "File\tMissed Lines\tFile\tPercent missed\n")
	for i := 0; i < limit; i++ {
		fmt.Fprintf(tabs, "%s\t%d\t%s\t%.2f\n",
			rps[i][0].filename, rps[i][0].MissCount(), rps[i][1].filename, rps[i][1].MissFraction()*100)
	}
	tabs.Flush()
}

func ingestCoverageFile(ctx *context, coverageRoot, projRoot, fp string) {
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
		ingestCoverageFile(&ctx, coverageRoot, projRoot, fp)
	}

	for f, r := range ctx.Results {
		buildFileCoverage(&ctx, projRoot, f, r)
	}

	ctx.Now = time.Now().Unix()
	return ctx
}

func parseOpts() options {
	parsed, err := docopt.Parse(docstring, nil, true, "", false)
	if err != nil {
		log.Fatal(err)
	}

	pwd, pwdErr := os.Getwd()
	opts := options{
		coverage: filepath.Join(os.Getenv("GOPATH"), "src"),
		project:  pwd,
	}
	log.Printf("%# v", opts)
	err = coerce.Struct(&opts, parsed, "-%s", "--%s", "<%s>")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%# v", opts)
	if opts.project == "" {
		log.Fatal(pwdErr)
	}
	return opts
}

func writeOut(ctx context, opts options) {
	tmpl := getTemplate("coverage.tmpl")

	out := &bytes.Buffer{}
	file, err := os.Create(opts.outpath)
	if err != nil {
		log.Fatal(err)
	}

	tmpl.Execute(out, ctx)

	_, err = file.Write(out.Bytes())
	if err != nil {
		log.Fatal(err)
	}
}
