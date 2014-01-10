#!/bin/bash

cd "$(dirname "$0")/.."

function list_external_packages() {
  go list -f '{{join .Deps "\n"}}' | xargs go list -f '{{if not .Standard}}{{.ImportPath}}{{end}}'
}

list_external_packages | while read package; do
  if [ ! -d "vendor/src/${package}" ]; then
    echo "${package}"
  fi
done
