# tra-commute-bot: a single static binary, run as a long-lived process
# (§4.2) — no cron, no systemd timer inside the container at all.

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
# CGO_ENABLED=0 gives a static binary: nothing on the host is needed, not even
# tzdata, which internal/../cmd/tracommute embeds via `import _ "time/tzdata"`.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/tracommute ./cmd/tracommute

# The distroless final stage has no shell to mkdir/chown with, and the
# nonroot user (65532:65532) needs to own /var/lib/tra-commute before a named
# volume is ever mounted there — Docker seeds a new volume's ownership from
# whatever is already at that path in the image, so this empty, pre-owned
# directory is what makes state.json and settings.json writable later.
RUN mkdir -p /var/lib/tra-commute && chown 65532:65532 /var/lib/tra-commute

# distroless/static, not scratch: the program makes HTTPS calls to TDX and the
# Telegram Bot API, and TLS verification needs a CA bundle that scratch does
# not carry. distroless adds that (and tzdata's system copy, unused here since
# the binary embeds its own) and nothing else — no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/tracommute /usr/local/bin/tracommute
COPY configs/config.example.yaml /etc/tra-commute/config.yaml
COPY --from=builder --chown=65532:65532 /var/lib/tra-commute /var/lib/tra-commute

ENV TZ=Asia/Taipei
VOLUME ["/var/lib/tra-commute"]

ENTRYPOINT ["/usr/local/bin/tracommute"]
CMD ["-config", "/etc/tra-commute/config.yaml", "-env-file", ""]
