#!/bin/sh
# Rewrites THIRD_PARTY_NOTICES.txt: the licence texts of the Go toolchain and of every module linked into
# the binary on any supported OS. Run it when dependencies change (needs the module cache: `go mod download`).
set -eu
cd "$(dirname "$0")/.."
out=THIRD_PARTY_NOTICES.txt
list=$(for os in darwin linux windows; do
  GOOS=$os go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' .
done | sort -u)
{
  echo "bonagram is MIT licensed (see LICENSE). It includes the Go runtime and the modules below,"
  echo "which are distributed under their own licences, reproduced here."
  echo
  echo "================================================================"
  echo "The Go programming language"
  echo "================================================================"
  if [ -f "$(go env GOROOT)/LICENSE" ]; then
    cat "$(go env GOROOT)/LICENSE"
  else # some distributions (Homebrew) leave it out; golang.org/x/* carries the identical text
    cat "$(echo "$list" | awk '$1 == "golang.org/x/sys" {print $3; exit}')/LICENSE"
  fi
  echo "$list" | while read -r path ver dir; do
    echo
    echo "================================================================"
    echo "$path $ver"
    echo "================================================================"
    found=0
    for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      if [ -f "$f" ]; then
        cat "$f"
        echo
        found=1
      fi
    done
    if [ "$found" = 0 ]; then # no licence file in the module: quote what its README says
      echo "(this module ships no licence file; its README says:)"
      awk 'tolower($0) ~ /^#+ *licen[sc]e/ {p=1} p' "$dir/README.md" | head -8
    fi
  done
} > "$out"
echo "$out: $(echo "$list" | wc -l | tr -d ' ') modules, $(wc -c < "$out" | tr -d ' ') bytes"
