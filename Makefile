VERSION  := $(shell cat VERSION)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
LAST_TAG := $(shell git describe --tags --abbrev=0 --match 'v*' 2>/dev/null)

.PHONY: build build-go web test check lint fmt vet dev-backend dev-web e2e clean \
	deploy-sync deploy-sync-check docker api-compat

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
	@! grep -rn --include='*.go' --include='*.ts' --include='*.tsx' --include='*.sh' --exclude-dir=node_modules --exclude='*_test.go' --exclude='*.spec.ts' -e 'dangerously-skip-permissions' -e 'bypassPermissions' -e 'dangerously-bypass-approvals-and-sandbox' -e 'dangerously-bypass-hook-trust' . || (echo 'forbidden flag found' && exit 1)

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

# CI check: fails when docs/openapi.yaml changed in a non-additive way
# since the last v* release tag reachable from HEAD (see
# hack/openapi-compat and docs/API.md's compatibility promise). Skips
# with a notice, rather than failing, when no such tag exists yet (e.g. a
# fresh checkout before Styr's first release).
api-compat:
	@if [ -z "$(LAST_TAG)" ]; then \
		echo "api-compat: no v* tag reachable from HEAD; skipping"; \
	else \
		echo "api-compat: comparing docs/openapi.yaml against $(LAST_TAG)"; \
		tmp=$$(mktemp); \
		git show "$(LAST_TAG):docs/openapi.yaml" > "$$tmp" 2>/dev/null || { echo "api-compat: $(LAST_TAG) has no docs/openapi.yaml; skipping"; rm -f "$$tmp"; exit 0; }; \
		go run ./hack/openapi-compat "$$tmp" docs/openapi.yaml; status=$$?; \
		rm -f "$$tmp"; \
		exit $$status; \
	fi

docker:
	docker build -f deploy/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		-t styr:$(VERSION) -t styr:latest .

clean:
	rm -rf bin web/dist/*
