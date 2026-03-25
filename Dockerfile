# Stage 1: Build UI
FROM node:22-alpine AS ui-builder
WORKDIR /app
COPY web/webmail/ web/webmail/
COPY web/admin/ web/admin/
COPY web/account/ web/account/
RUN if [ -f web/webmail/package.json ]; then cd web/webmail && npm ci && npm run build; fi
RUN if [ -f web/admin/package.json ]; then cd web/admin && npm ci && npm run build; fi
RUN if [ -f web/account/package.json ]; then cd web/account && npm ci && npm run build; fi

# Stage 2: Build Go binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /app/web/webmail/dist web/webmail/dist 2>/dev/null || true
COPY --from=ui-builder /app/web/admin/dist web/admin/dist 2>/dev/null || true
COPY --from=ui-builder /app/web/account/dist web/account/dist 2>/dev/null || true
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /umailserver ./cmd/umailserver

# Stage 3: Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go-builder /umailserver /usr/local/bin/umailserver
EXPOSE 25 465 587 143 993 80 443 8443 3000
VOLUME /var/lib/umailserver
ENTRYPOINT ["umailserver"]
CMD ["serve"]
