.PHONY: build build-backend-only generate test test-unit test-integration test-e2e

VERSION ?= $(shell tr -d '\r\n' < ./cmd/server/VERSION)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)
DIST_DIR := internal/web/dist

# 可部署产物：必须带 -tags embed，否则前端不会被嵌进去，
# 跑起来 API 全正常但首页 404（internal/web/embed_off.go）。
build:
	@test -d $(DIST_DIR) || { \
		echo "错误：$(DIST_DIR) 不存在，前端尚未构建。"; \
		echo "先在仓库根目录跑：make build-frontend"; \
		exit 1; }
	CGO_ENABLED=0 go build -tags embed -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

# 只编后端、不嵌前端（本地调后端用）。产物刻意换名字，
# 避免和可部署的 bin/server 混在一起被误上传。
build-backend-only:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server-noembed ./cmd/server
	@echo "注意：bin/server-noembed 不含前端，仅供本地调试，请勿部署"

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-integration:
	go test -tags=integration ./...

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
