#!/bin/sh
# Renders the fixture and checks every config with the pinned sing-box.
set -eu
state=${1:-testdata/basic}
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

version=$(grep -A2 '^sing-box:' "$state/versions.yaml" | awk '/version:/{print $2; exit}')
sha=$(grep -A3 '^sing-box:' "$state/versions.yaml" | awk '/sha256:/{print $2; exit}')
if [ -z "$version" ] || [ -z "$sha" ]; then
  echo "sing-box version or sha256 is not pinned in $state/versions.yaml: run vpnctl versions pin" >&2
  exit 1
fi

url="https://github.com/SagerNet/sing-box/releases/download/v${version}/sing-box-${version}-linux-amd64.tar.gz"
echo "downloading sing-box ${version}"
curl -fsSL --retry 3 -o "$dir/sing-box.tgz" "$url"
echo "${sha}  $dir/sing-box.tgz" | sha256sum -c --quiet
tar -xzf "$dir/sing-box.tgz" -C "$dir"
bin="$dir/sing-box-${version}-linux-amd64/sing-box"

# the configs point at certificates the host has; the check needs a throwaway one
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
  -subj "/CN=check" -keyout "$dir/self.key" -out "$dir/self.crt" >/dev/null 2>&1

go run ./cmd/vpnctl render -dir "$state" --out "$dir/render" >/dev/null

for cfg in "$dir"/render/*/etc/sing-box/config.json; do
  [ -f "$cfg" ] || continue
  root=$(dirname "$(dirname "$(dirname "$cfg")")")
  mkdir -p "$root/etc/sing-box/rules"
  # rule sets are downloaded on the host by bootstrap; take them from the release
  for rel in $(grep -o 'rules/[a-z0-9-]*\.srs' "$cfg" | sort -u); do
    tag=$(basename "$rel" .srs)
    repo=sing-geosite
    case "$tag" in geoip-*) repo=sing-geoip ;; esac
    curl -fsSL --retry 3 "https://raw.githubusercontent.com/SagerNet/$repo/rule-set/$tag.srs" \
      -o "$root/etc/sing-box/$rel" || { echo "cannot fetch $tag" >&2; exit 1; }
  done
  # any certificate path becomes the throwaway pair
  sed -E "s#\"(/etc/vpnctl/self|/var/lib/caddy)[^\"]*\.crt\"#\"$dir/self.crt\"#g; s#\"(/etc/vpnctl/self|/var/lib/caddy)[^\"]*\.key\"#\"$dir/self.key\"#g" \
    "$cfg" > "$dir/check.json"
  "$bin" check -D "$root/etc/sing-box" -c "$dir/check.json" || { echo "check failed: $cfg" >&2; exit 1; }
  echo "ok: ${cfg#"$dir"/render/}"
done
