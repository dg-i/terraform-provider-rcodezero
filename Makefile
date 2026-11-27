VERSION  ?= 0.1.0
OS_ARCH  := $(shell go env GOOS)_$(shell go env GOARCH)
PROVIDER := registry.terraform.io/dg-i/rcodezero
PLUGINS  := $(HOME)/.terraform.d/plugins/$(PROVIDER)/$(VERSION)/$(OS_ARCH)

# Same version as in .github/workflows/test.yml and .pre-commit-config.yaml.
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: generate build test testacc lint install mock tidy

# Regenerates the API client and mock server from api/, and docs/ via tfplugindocs.
generate:
	go generate ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/terraform-provider-rcodezero .

test:
	go test ./...

# Tests that drive real tofu commands against the in-process mock.
testacc:
	TF_ACC=1 go test ./internal/acceptance/ -v -timeout 300s

# Installs into the implied local mirror, so `tofu init` finds it without a registry.
install: build
	mkdir -p $(PLUGINS)
	cp bin/terraform-provider-rcodezero $(PLUGINS)/terraform-provider-rcodezero_v$(VERSION)

mock:
	go run ./cmd/mock-rcodezero

tidy:
	go mod tidy
