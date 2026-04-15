# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git nodejs npm

WORKDIR /app

# Copy Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy web assets and build frontend
COPY webmail/ ./webmail/
COPY web/admin/ ./web/admin/
COPY web/account/ ./web/account/

# Build webmail
WORKDIR /app/webmail
RUN npm ci && npm run build

# Build admin
WORKDIR /app/web/admin
RUN npm ci && npm run build

# Build account
WORKDIR /app/web/account
RUN npm ci && npm run build

# Build Go binary
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o umailserver ./cmd/umailserver

# Runtime stage
FROM alpine:latest

# Install ca-certificates for TLS
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 1000 umailserver && \
    adduser -u 1000 -G umailserver -s /bin/sh -D umailserver

# Copy binary from builder
COPY --from=builder /app/umailserver /usr/local/bin/umailserver
RUN chmod +x /usr/local/bin/umailserver

# Create data directory
RUN mkdir -p /var/lib/umailserver /etc/umailserver && \
    chown -R umailserver:umailserver /var/lib/umailserver

# Switch to non-root user
USER umailserver

# Expose ports
EXPOSE 25 465 587 143 993 995 4190 443 8443 8080 3000

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the server
ENTRYPOINT ["umailserver"]
CMD ["serve", "--config", "/etc/umailserver/umailserver.yaml"]
