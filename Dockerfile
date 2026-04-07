# Multi-stage build for uMailServer
# Stage 1: Build the webmail frontend
FROM node:20-alpine AS webmail-builder

WORKDIR /app/webmail
COPY webmail/package*.json .
RUN npm ci

COPY webmail/ .
RUN npm run build

# Stage 2: Build the admin panel
FROM node:20-alpine AS admin-builder

WORKDIR /app/web/admin
COPY web/admin/package*.json .
RUN npm ci

COPY web/admin/ .
RUN npm run build

# Stage 3: Build the Go binary
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git make

WORKDIR /app

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy built web assets from previous stages
COPY --from=webmail-builder /app/webmail/dist ./webmail/dist
COPY --from=admin-builder /app/web/admin/dist ./web/admin/dist

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=$(git describe --tags --always) -X main.BuildDate=$(date -u +%Y-%m-%d)" -o umailserver ./cmd/umailserver

# Stage 4: Final minimal image
FROM alpine:3.20

LABEL org.opencontainers.image.title="uMailServer"
LABEL org.opencontainers.image.description="One binary. Complete email."
LABEL org.opencontainers.image.source="https://github.com/umailserver/umailserver"

# Install ca-certificates for TLS
RUN apk add --no-cache ca-certificates

# Create user and directories
RUN adduser -D -u 1000 umail && \
    mkdir -p /data /etc/umailserver && \
    chown -R umail:umail /data

# Copy binary from builder
COPY --from=go-builder /app/umailserver /usr/local/bin/umailserver

# Set permissions
RUN chmod +x /usr/local/bin/umailserver

# Switch to non-root user
USER umail

# Expose ports
# 25 - SMTP (inbound)
# 587 - SMTP Submission (STARTTLS)
# 465 - SMTP Submission (TLS)
# 993 - IMAPS
# 8443 - Admin API
EXPOSE 25 587 465 993 8443

# Data volume
VOLUME ["/data"]

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8443/health || exit 1

# Run the server
ENTRYPOINT ["umailserver"]
CMD ["serve"]
