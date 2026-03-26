.PHONY: build test clean lint run docker coverage

BINARY_NAME=umailserver
DOCKER_IMAGE=umailserver

build:
	go build -o $(BINARY_NAME) ./cmd/umailserver

test:
	go test -v ./...

test-race:
	go test -v -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

run:
	go run ./cmd/umailserver

clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html

docker:
	docker build -t $(DOCKER_IMAGE):latest .

docker-run:
	docker run -p 25:25 -p 587:587 -p 8080:8080 -v $(PWD)/data:/data $(DOCKER_IMAGE):latest

fmt:
	go fmt ./...

vet:
	go vet ./...

deps:
	go mod download
	go mod tidy

all: clean build test
