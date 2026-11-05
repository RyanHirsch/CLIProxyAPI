# Layers fork plugins onto the image built from upstream's unchanged Dockerfile.
# deploy.sh builds that image first and tags it cli-proxy-api-upstream:local.
FROM golang:1.26-bookworm AS plugins

WORKDIR /src

COPY go.mod go.sum ./
COPY fork-plugins/account-routing/go.mod fork-plugins/account-routing/go.sum fork-plugins/account-routing/
RUN cd fork-plugins/account-routing && go mod download

COPY . .
RUN cd fork-plugins/account-routing && CGO_ENABLED=1 go build -buildvcs=false -buildmode=c-shared -o /out/account-routing.so .

FROM cli-proxy-api-upstream:local

COPY --from=plugins /out/account-routing.so /CLIProxyAPI/plugins/account-routing.so
