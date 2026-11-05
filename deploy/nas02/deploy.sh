#!/usr/bin/env bash
set -euo pipefail

nas=nas02.donkey-bushi.ts.net
deploy_dir=/volume1/docker/cli-api-proxy
url=https://cli-api-proxy.donkey-bushi.ts.net
nas_image=localhost:5050/cli-proxy-api
nas_docker="sudo -n /usr/local/bin/docker"
upstream=https://github.com/router-for-me/CLIProxyAPI.git

cd "$(dirname "$0")"

git fetch --quiet origin main
git fetch --quiet --tags "$upstream"
if [[ -n "$(git status --porcelain)" ]]; then
	echo "Refusing to deploy: the working tree has uncommitted or untracked changes." >&2
	exit 1
fi
if [[ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]]; then
	echo "Refusing to deploy: HEAD is not origin/main. Merge to main, pull, then deploy." >&2
	exit 1
fi

VERSION="$(git describe --tags --always)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

trap 'git -C ../.. checkout --quiet -- internal/registry/models' EXIT
(cd ../.. && bash .github/scripts/refresh-model-catalogs.sh)
docker build --platform linux/amd64 --tag cli-proxy-api-upstream:local \
	--build-arg VERSION="$VERSION" --build-arg COMMIT="$COMMIT" --build-arg BUILD_DATE="$BUILD_DATE" ../..
MANAGEMENT_PASSWORD=build-only docker compose build cli-api-proxy
MANAGEMENT_PASSWORD=build-only docker compose push cli-api-proxy

nas_compose() { ssh "$nas" "cd $deploy_dir && $nas_docker compose $*"; }

verify() {
	curl --fail --silent --show-error --retry 5 --retry-all-errors --retry-delay 2 --output /dev/null "$url/" &&
		nas_compose logs --no-log-prefix cli-api-proxy | grep -q "Commit: $COMMIT,"
}

for file in compose.yaml tailscale-serve.json; do
	ssh "$nas" "cat > $deploy_dir/$file" < "$file"
done
ssh "$nas" "$nas_docker image tag $nas_image $nas_image:previous 2>/dev/null || true"
nas_compose pull cli-api-proxy
nas_compose up -d --wait --wait-timeout 120

if ! verify; then
	echo "Verification failed for $VERSION; rolling back to $nas_image:previous." >&2
	ssh "$nas" "$nas_docker image tag $nas_image:previous $nas_image"
	nas_compose up -d --wait --wait-timeout 120
	exit 1
fi
echo "Deployed $VERSION ($COMMIT) to $url"
