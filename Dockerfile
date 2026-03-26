FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o umailserver ./cmd/umailserver

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/umailserver .
COPY --from=builder /app/webmail/public ./webmail/public

EXPOSE 25 587 110 995 143 993 8080

VOLUME ["/data"]

ENTRYPOINT ["./umailserver"]
CMD ["serve", "--config", "/data/config.yaml"]
