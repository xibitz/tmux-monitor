.PHONY: setup web build package check test dev bootstrap clean

setup:
	npm ci --prefix apps/web
	go mod download

web:
	find internal/webassets/dist -mindepth 1 ! -name .gitkeep -delete
	npm run build --prefix apps/web

build: web
	go build -trimpath -o bin/hub ./cmd/hub
	go build -trimpath -o bin/agent ./cmd/agent

package: build
	tar -czf "bin/tmux-monitor-$$(go env GOOS)-$$(go env GOARCH).tar.gz" \
		-C "$(CURDIR)/bin" hub agent \
		-C "$(CURDIR)" deploy docs README.md PRD.md IMPLEMENTATION_PLAN.md MEMORY.md .env.example \
		scripts/hub-backup.py scripts/measure-capacity.py

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
