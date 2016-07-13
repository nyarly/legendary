// This file was automatically generated based on the contents of *.tmpl
// If you need to update this file, change the contents of those files
// (or add new ones) and run 'go generate'

package main

const (
	coverageTmpl = "let s:generatedTime = {{ .Now }}\nlet s:coverageResults = {\n{{ range $file, $coverage := .Results }}\\'{{ $file }}': {\n\\  'hits': [\n{{- range .Hits -}}\n{{.}},\n{{- end -}}\n],\n\\  'misses': [\n{{- range .Misses -}}\n{{.}},\n{{- end -}}\n],\n\\  'ignored': [\n{{- range .Ignored -}}\n{{.}},\n{{- end -}}\n],\n\\  },\n{{ end -}}\n\\}\ncall AddSimplecovResults(expand(\"<sfile>:p\"), s:coverageResults)\n"
)
