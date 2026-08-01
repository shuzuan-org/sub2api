.PHONY: wire build build-embed test-unit test-integration test-e2e test-cover-integration

wire:
	@echo "生成 Wire 代码..."
	@cd cmd/server && go generate
	@echo "Wire 代码生成完成"

# 不嵌前端的产物换个名字，避免和可部署的 bin/server 撞名后被误上传
# （不含前端的二进制跑起来 API 全正常，只有首页 404，很难第一时间发现）。
build:
	@echo "构建后端（不嵌入前端，仅供本地调试）..."
	@go build -o bin/server-noembed ./cmd/server
	@echo "构建完成: bin/server-noembed（不含前端，请勿部署）"

build-embed:
	@echo "构建后端（嵌入前端）..."
	@go build -tags embed -o bin/server ./cmd/server
	@echo "构建完成: bin/server (with embedded frontend)"

test-unit:
	@go test -tags unit ./... -count=1

test-integration:
	@go test -tags integration ./... -count=1 -race -parallel=8

test-e2e:
	@echo "运行 E2E 测试（需要本地服务器运行）..."
	@go test -tags e2e ./internal/integration/... -count=1 -v

test-cover-integration:
	@echo "运行集成测试并生成覆盖率报告..."
	@go test -tags=integration -cover -coverprofile=coverage.out -count=1 -race -parallel=8 ./...
	@go tool cover -func=coverage.out | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

clean-coverage:
	@rm -f coverage.out coverage.html
	@echo "覆盖率文件已清理"

clean: clean-coverage
	@rm -rf bin/
	@echo "构建产物已清理"