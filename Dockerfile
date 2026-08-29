# One image, several roles. The compose stack runs it as a player, a conductor,
# an instrument node and the studio, which is far easier to reason about than
# four images that must be kept in step.
#
# It carries mpv, ffmpeg and Python because a demonstration that cannot play a
# film or generate a score is not demonstrating much.

FROM golang:1.24-trixie AS build
WORKDIR /src

# Dependencies first, so editing source does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The studio's HTML, CSS and JavaScript are embedded with go:embed, so the
# binary is genuinely the whole program.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/componium ./cmd/componium

FROM debian:trixie-slim

# mpv is the reference time source, ffmpeg is what the composer decodes with,
# and python3 runs the composer itself. No recommends: this is a headless box
# and pulling in a desktop stack would quadruple the image.
RUN apt-get update \
 && apt-get install -y --no-install-recommends mpv ffmpeg python3 ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/componium /usr/local/bin/componium
COPY composer/ /opt/componium/composer/
COPY examples/ /opt/componium/examples/
COPY hack/ /opt/componium/hack/

WORKDIR /opt/componium

# Runs as a normal user. Nothing here needs root, and a container that drives
# physical hardware is exactly the wrong place to be casual about that.
#
# Every directory a named volume will be mounted on must exist here, owned by
# that user. Docker seeds a fresh named volume from the image path it covers,
# ownership included, but only when that path already exists; otherwise it
# creates it root-owned and a non-root container cannot write to it.
#
# /run/componium is the exception: it is 1777, like /tmp, because it is shared
# scratch space rather than one user's directory. The compose file overrides
# `user:` to whoever owns the host's media directory, so the uid that creates
# the mpv socket is not known when this image is built — and a fresh named
# volume inherits this path's ownership, so a 10001-owned directory means the
# player silently cannot create its socket. What that looked like: mpv ran
# happily with no IPC at all, a stale socket from an older deployment satisfied
# the conductor's `[ -S ]` guard, and the conductor then failed to connect to
# it forever. Sticky and world-writable is the /tmp pattern and it is the right
# one here: any uid may create the socket, and none may remove another's.
RUN useradd --create-home --uid 10001 componium \
 && mkdir -p /run/componium /scores /media \
 && chown -R componium:componium /run/componium /scores /media /opt/componium \
 && chmod 1777 /run/componium
USER componium

LABEL org.opencontainers.image.title="Componium" \
      org.opencontainers.image.description="Show control for 4D home cinema" \
      org.opencontainers.image.source="https://github.com/Slicit/componium" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

ENTRYPOINT ["componium"]
CMD ["--help"]
