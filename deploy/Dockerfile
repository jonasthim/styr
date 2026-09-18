# Styr: one Go binary with an embedded React SPA. Build from the repo root:
#
#   docker build -f deploy/Dockerfile -t styr:dev .
#
# Three stages: the web SPA, the Go binary (which embeds the SPA's dist/),
# and a slim Debian runtime with git, ssh and the Claude Code CLI the
# server shells out to.

# ---- stage 1: web -----------------------------------------------------------
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- stage 2: go binary ------------------------------------------------------
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/styr ./cmd/styr

# ---- stage 3: runtime ---------------------------------------------------------
FROM debian:bookworm-slim AS runtime

RUN apt-get update -qq \
    && apt-get install -y -qq --no-install-recommends \
       git openssh-client curl ca-certificates tini \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --uid 1000 --create-home --home-dir /var/lib/styr --shell /usr/sbin/nologin styr \
    && install -d -o styr -g styr -m 0750 /var/lib/styr

COPY --from=build /out/styr /usr/local/bin/styr

# Install the Claude Code CLI for the styr user (the server shells out to
# `claude` under this account); ENV PATH below makes the result reachable
# for the ENTRYPOINT below without guessing the installer's exact layout.
USER styr
ENV HOME=/var/lib/styr
ENV PATH="/var/lib/styr/.local/bin:${PATH}"
RUN curl -fsSL https://claude.ai/install.sh | bash \
    && claude --version

ENV STYR_LISTEN=0.0.0.0:8080 \
    STYR_DATA_DIR=/var/lib/styr

VOLUME /var/lib/styr
EXPOSE 8080

ENTRYPOINT ["tini", "--", "styr"]
CMD ["serve"]
