# syntax=docker/dockerfile:1.7

########################
# Build stage (pcap + goproxy.cn)
########################
ARG GO_VERSION=1.25.3
FROM --platform=${TARGETPLATFORM} golang:${GO_VERSION}-alpine AS builder

# pcap 需要 CGO；build-base 提供 gcc/ld；pkgconf 帮 cgo 找到 pcap；git 备 direct 回退
RUN apk add --no-cache build-base libpcap-dev pkgconf ca-certificates tzdata git

# 使用国内代理（直连回退）
ENV CGO_ENABLED=1 \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn

WORKDIR /src

# 先拉依赖（缓存模块）
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# 拷贝源码并构建；产物输出到 /out/portnote
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/portnote ./cmd/server


########################
# Runtime stage (Alpine)
########################
FROM --platform=${TARGETPLATFORM} alpine:3.22.2

# 运行期仅需 pcap 动态库 + 证书（tzdata 可选）
RUN apk upgrade --no-cache && apk add --no-cache libpcap ca-certificates tzdata

WORKDIR /app

# 二进制与 web 同目录，程序可使用相对路径 ./web
COPY --from=builder /out/portnote /app/portnote
COPY web /app/web

# 非 root 运行
RUN adduser -D -H -s /sbin/nologin portnote \
 && chown -R portnote:portnote /app \
 && chmod 0755 /app/portnote
USER portnote

# 运行配置（按需覆盖）
ENV PORTNOTE_HTTP_ADDR="0.0.0.0:8080" \
    PORTNOTE_DB_PATH="/app/data/portnote.db"

EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/portnote"]