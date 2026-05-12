VERSION ?= $(shell grep 'const version = ' cmd/agent/main.go | sed 's/.*"\(.*\)".*/\1/')

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
LDFLAGS := -s -w -X main.version=$(VERSION)
BINARY := nodestral-agent

.PHONY: build build-all test lint release tag patch minor major

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/agent

build-all:
	@for target in $(PLATFORMS); do \
		GOOS=$${target%/*} GOARCH=$${target##*/} CGO_ENABLED=0 \
			go build -buildvcs=false -ldflags="$(LDFLAGS)" \
			-o $(BINARY)-$${target%/*}-$${target##*/} ./cmd/agent; \
		echo "Built: $(BINARY)-$${target%/*}-$${target##*/}"; \
	done

test:
	go test ./...

lint:
	golangci-lint run ./... 2>/dev/null || go vet ./...

# Version bumping
patch: ## v0.1.0 -> v0.1.1
	@$(MAKE) bump PART=patch

minor: ## v0.1.0 -> v0.2.0
	@$(MAKE) bump PART=minor

major: ## v0.1.0 -> v1.0.0
	@$(MAKE) bump PART=major

bump:
	@bash -c 'CURRENT=$(VERSION) && \
	IFS="." read -r MAJOR MINOR PATCH <<< "$$CURRENT" && \
	case $(PART) in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
	esac && \
	NEW="$$MAJOR.$$MINOR.$$PATCH" && \
	echo "$$CURRENT -> $$NEW" && \
	sed -i "s/const version = \".*\"/const version = \"$$NEW\"/" cmd/agent/main.go && \
	git add cmd/agent/main.go && \
	git commit -m "release v$$NEW" && \
	git tag "v$$NEW" && \
	echo "Committed and tagged v$$NEW. Run: git push origin main --tags"'
