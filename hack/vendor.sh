#!/bin/bash
set -e

cd "$(dirname "$0")/.."

rm -fr vendor
mkdir vendor
cd vendor

function get_path() {
  echo "${1#*://}"
}

function git_clone() {
  local url="$1"
  local rev="$2"
  local path="$(get_path "${url}")"

  echo "Cloning ${url} ${rev} to ${path}"

  git clone -q "${url}" "${path}"
  (
    cd "${path}"
    git checkout -q "${rev}"
    git fsck
    rm -rf .git
  )
}

git_clone https://github.com/kevinwallace/crontab cb0c8ebeb8bc47a9ef819de83964e72b0cd48e69
git_clone https://github.com/kevinwallace/fieldsn 5d1f8e322e23b05814b8d2de7571d00d32d9d8cd

