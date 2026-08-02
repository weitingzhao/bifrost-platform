#!/usr/bin/env bash
set -euo pipefail

REGISTRY="192.168.10.73:30500"
IMAGE_NAME="bifrost-platform-api"
TAG="${1:-prod}"
FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${TAG}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
API_DIR="${REPO_ROOT}/api"
BUILD_DIR="${REPO_ROOT}/.docker-build"

echo "=== Building platform-api (linux/amd64) ==="
mkdir -p "$BUILD_DIR"
cd "$API_DIR"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o "${BUILD_DIR}/platform-api" ./cmd/platform-api

echo "=== Binary SHA256 ==="
shasum -a 256 "${BUILD_DIR}/platform-api"

echo "=== Building Docker image: ${IMAGE_NAME}:${TAG} ==="
cat > "${BUILD_DIR}/Dockerfile" <<'EOF'
FROM scratch
COPY platform-api /usr/local/bin/platform-api
ENTRYPOINT ["/usr/local/bin/platform-api"]
EOF

docker build --platform linux/amd64 -t "${IMAGE_NAME}:${TAG}" "$BUILD_DIR"

echo "=== Pushing to registry via skopeo ==="
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock quay.io/skopeo/stable:latest \
  copy --dest-tls-verify=false \
  "docker-daemon:${IMAGE_NAME}:${TAG}" \
  "docker://${FULL_IMAGE}"

echo "=== Verifying push ==="
docker run --rm quay.io/skopeo/stable:latest \
  inspect --tls-verify=false "docker://${FULL_IMAGE}" | head -20

echo ""
echo "=== SUCCESS ==="
echo "Image pushed: ${FULL_IMAGE}"
echo ""
echo "Next steps:"
echo "  kubectl -n bifrost-prod rollout restart deployment/platform-api"
echo "  kubectl -n bifrost-prod rollout status  deployment/platform-api"
