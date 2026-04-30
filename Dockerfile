# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o umailserver ./cmd/umailserver

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

# Create user and directories
RUN addgroup -g 1000 umailserver && \
    adduser -u 1000 -G umailserver -s /bin/sh -D umailserver && \
    mkdir -p /var/lib/umailserver /etc/umailserver /home/umailserver/certs && \
    chown -R umailserver:umailserver /var/lib/umailserver /home/umailserver

COPY --from=builder /app/umailserver /usr/local/bin/umailserver
RUN chmod +x /usr/local/bin/umailserver

# Set working directory
WORKDIR /home/umailserver
USER umailserver

EXPOSE 25 465 587 143 993 995 4190 443 8443 8080 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["umailserver"]
CMD ["serve", "--config", "/etc/umailserver/umailserver.yaml"]
