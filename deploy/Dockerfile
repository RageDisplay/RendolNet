# Все образы RendolNet из одного файла, нужный выбирается через --target:
#   api      rendol-api с веб-интерфейсом
#   agent    rendol-agent
#   zapret   nfqws для модуля обхода DPI
#   host     бинари для узла (rendol-updater, rendol-console, rendol-installer),
#            забираются через --output
#   agent-dev агент с iproute2 для стенда deploy/dev

ARG GO_IMAGE=golang:1.26.8
ARG NODE_IMAGE=node:22-alpine
ARG DEBIAN_IMAGE=debian:trixie-slim

FROM ${NODE_IMAGE} AS web
ARG NPM_REGISTRY=
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund --registry="${NPM_REGISTRY:-https://registry.npmjs.org/}"
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS build
ARG GOPROXY=
ARG GOFLAGS=-buildvcs=false
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct} GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd cmd
COPY internal internal
ARG VERSION=dev
ARG BINS="rendol-api rendol-agent rendol-updater rendol-console rendol-installer rendolctl"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for b in ${BINS}; do \
      go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/$b ./cmd/$b || exit 1; \
    done

FROM scratch AS host
COPY --from=build /out/rendol-updater /out/rendol-console /out/rendol-installer /out/rendolctl /

FROM ${DEBIAN_IMAGE} AS zapret-base
ARG APT_MIRROR=
RUN S=/etc/apt/sources.list.d/debian.sources \
 && sed -i "s#^URIs: http://deb.debian.org/debian\$#URIs: ${APT_MIRROR:-http://deb.debian.org/debian}#" $S \
 && apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates nftables \
      libnetfilter-queue1 libnfnetlink0 libmnl0 zlib1g libcap2 \
 && rm -rf /var/lib/apt/lists/*

FROM zapret-base AS zapret-build
ARG ZAPRET_REF=v72.13
ARG FLOWSEAL_REF=1.10.3
RUN apt-get update \
 && apt-get install -y --no-install-recommends git make gcc libc6-dev curl unzip \
      libnetfilter-queue-dev libnfnetlink-dev libmnl-dev zlib1g-dev libcap-dev \
 && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "${ZAPRET_REF}" https://github.com/bol-van/zapret /zapret \
 && make -C /zapret/nfq \
 && install -m 0755 /zapret/nfq/nfqws /nfqws
RUN curl -fsSL -o /tmp/fs.zip "https://github.com/Flowseal/zapret-discord-youtube/archive/refs/tags/${FLOWSEAL_REF}.zip" \
 && cd /tmp && unzip -q fs.zip \
 && mkdir -p /opt/zapret \
 && cp -r /tmp/zapret-discord-youtube-*/lists /opt/zapret/lists \
 && mkdir /opt/zapret/bin && cp /tmp/zapret-discord-youtube-*/bin/*.bin /opt/zapret/bin/ \
 && cp /tmp/zapret-discord-youtube-*/LICENSE.txt /opt/zapret/LICENSE.flowseal
COPY --from=build /out/rendol-agent /app/rendol-agent

FROM zapret-build AS zapret-check
RUN /app/rendol-agent zapret-check /nfqws /opt/zapret > /checked

FROM zapret-base AS zapret
COPY --from=zapret-check /checked /opt/zapret/.checked
COPY --from=zapret-build /nfqws /app/nfqws
COPY --from=zapret-build /opt/zapret /opt/zapret
COPY --from=build /out/rendol-agent /app/rendol-agent
ENTRYPOINT ["/app/rendol-agent", "zapret"]

FROM gcr.io/distroless/static-debian12 AS agent
COPY --from=build /out/rendol-agent /app/rendol-agent
ENTRYPOINT ["/app/rendol-agent"]

FROM ${DEBIAN_IMAGE} AS agent-dev
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 nftables && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/rendol-agent /app/rendol-agent
COPY deploy/dev/agent-entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]

# api идёт последним: это цель по умолчанию
FROM gcr.io/distroless/static-debian12 AS api
COPY --from=build /out/rendol-api /app/rendol-api
COPY --from=web /web/dist /app/web
WORKDIR /app
ENTRYPOINT ["/app/rendol-api"]
