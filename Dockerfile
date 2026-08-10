FROM node:22-alpine AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund --registry=https://registry.npmmirror.com
COPY web/ .
RUN npm run build

FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/uno-server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 uno \
    && adduser -S -D -H -u 10001 -G uno uno
COPY --from=builder /out/uno-server /usr/local/bin/uno-server
COPY --from=web-builder /src/web/dist /srv/web

USER uno:uno
ENV UNO_WEB_DIR=/srv/web
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/uno-server"]
