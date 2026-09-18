VERSION := $(shell cat VERSION)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build build-go web test check lint fmt vet dev-backend dev-web e2e clean \
	deploy-sync deploy-sync-check docker

build: web build-go

build-go:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/styr ./cmd/styr

web:
	cd web && npm ci && npm run build

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w $$(git ls-files '*.go')

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...

check: vet test
	@! grep -rn --include='*.go' --include='*.ts' --include='*.tsx' --include='*.sh' --exclude-dir=node_modules --exclude='*_test.go' --exclude='*.spec.ts' -e 'dangerously-skip-permissions' -e 'bypassPermissions' . || (echo 'forbidden flag found' && exit 1)

dev-backend:
	STYR_ENV=dev STYR_CONFIG=dev.config.yaml go run ./cmd/styr serve

dev-web:
	cd web && npm run dev

e2e:
	cd web && npx playwright test

deploy-sync:
	./deploy/build-installer.sh

# CI check: fails if deploy/install.sh was not regenerated after an edit to
# deploy/install.sh.in or one of the files it embeds.
deploy-sync-check: deploy-sync
	git diff --exit-code -- deploy/install.sh || (echo "deploy/install.sh is out of date; run 'make deploy-sync' and commit it" && exit 1)

docker:
	docker build -f deploy/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		-t styr:$(VERSION) -t styr:latest .

clean:
	rm -rf bin web/dist/*
