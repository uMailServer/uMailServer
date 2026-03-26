# Build stage for Go backend
FROM golang:1.26-alpine AS go-builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o umailserver ./cmd/umailserver

# Build stage for React webmail
FROM node:22-alpine AS web-builder

WORKDIR /webmail
COPY webmail/package*.json ./
RUN npm ci

COPY webmail/ ./
RUN npm run build

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=go-builder /app/umailserver .
COPY --from=web-builder /webmail/dist ./webmail/public

EXPOSE 25 587 110 995 143 993 8080

VOLUME ["/data"]

ENTRYPOINT ["./umailserver"]
CMD ["serve", "--config", "/data/config.yaml"]
