.PHONY: setup web build check test dev bootstrap clean

setup:
	npm ci --prefix apps/web
	go mod download

web:
	find internal/webassets/dist -mindepth 1 ! -name .gitkeep -delete
	npm run build --prefix apps/web

build: web
	go build -trimpath -o bin/hub ./cmd/hub

check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...
	npm run check --prefix apps/web

test:
	go test -race ./...
	npm test --prefix apps/web

dev:
	TMUX_MONITOR_DEV=true TMUX_MONITOR_ORIGIN=http://127.0.0.1:5173 go run ./cmd/hub serve

bootstrap:
	go run ./cmd/hub bootstrap

clean:
	rm -rf bin
	find internal/webassets/dist -mindepth 1 ! -name .gitkeep -delete
