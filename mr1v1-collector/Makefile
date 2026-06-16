.PHONY: build gateway consumer agent backend fmt images clean

build: gateway consumer agent backend

gateway:
	CGO_ENABLED=0 go build -trimpath -o ./bin/gateway ./cmd/gateway

consumer:
	CGO_ENABLED=0 go build -trimpath -o ./bin/consumer ./cmd/consumer

agent:
	CGO_ENABLED=0 go build -trimpath -o ./bin/agent ./cmd/agent

backend:
	CGO_ENABLED=0 go build -trimpath -o ./bin/backend ./cmd/backend

fmt:
	gofmt -w .

images:
	docker compose build

clean:
	rm -rf ./bin
