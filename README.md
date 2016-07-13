# Legendary

This is a companion app to
https://github.com/killphi/vim-legend
that processes Go cover profiles into
coverage.vim files.

Use it like this:

```
go test -coverprofile=cover.out
legendary .cadre/coverage.vim cover.out
```

Enjoy coverage information that doesn't mess with your syntax highlighting in Go.

## Advanced Use

There's a known issue
(PR accepted for reference...)
with `go test` not being able to do coverage for multiple packages.
In other words
`go test -coverprofile=cover.out ./...`
fails with an explicit error.

Legendary will handle and merge multiple profiles,
so it's possible to do something like:

```
for p in $(go list ./...); do
  go test -coverprofile=/tmp/rootproj/(echo $p | sed 's#/##g').out $p
end
legendary .cadre/coverage.vim /tmp/rootproj/*.out
```
