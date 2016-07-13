# Legendary

This is a companion app to
https://github.com/killphi/vim-legend
that processes Go cover profiles into
coverage.vim files.

Use it like this:

```
for p in $(go list ./...); do
  go test -coverprofile=/tmp/rootproj/(echo $p | sed 's#/##g').out $p
end
legendary .cadre/coverage.vim /tmp/rootproj/*.out
```

Enjoy coverage information that doesn't mess with your syntax highlighting in Go.
