#!/bin/bash

cd "$(dirname "$0")/.."

local_package="$(go list)"

function list_external_packages() {
  go list -f '{{join .Deps "\n"}}' ./... | xargs go list -f '{{if not .Standard}}{{.ImportPath}}{{end}}'
}

list_external_packages | while read package; do
  if [[ "${package}" == "${local_package}/"* ]]; then
    continue
  fi
  if [ ! -d "vendor/src/${package}" ]; then
    echo "${package}"
  fi
done
