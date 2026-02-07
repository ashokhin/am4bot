GOVERSION	:= $(shell go env GOVERSION)
GOARCH		:= $(shell go env GOARCH)
GOOS		:= $(shell go env GOOS)

BIN_DIR		?= $(shell pwd)/bin/
EXEC_EXT	?= $(shell go env GOEXE)

AMBOT_BIN		:= ambot$(EXEC_EXT)
ROUTE_SCAN_BIN	:= scanner$(EXEC_EXT)

export APP_BRANCH		?= $(shell git describe --all --contains --dirty HEAD)
export APP_VERSION		:= $(shell basename ${APP_BRANCH})
export APP_REVISION		?= $(shell git rev-parse HEAD)
export APP_USER			:= $(shell id -u --name)
export APP_HOST			?= $(shell hostname)
export APP_BUILD_DATE	:= $(shell date -u '+%Y-%m-%dT%H:%M:%S,%N%:z')

all: clean format vet test build

clean:
	@echo ">> removing build artifacts"
	@rm -f $(BIN_DIR)$(AMBOT_BIN)
	@rm -f $(BIN_DIR)$(ROUTE_SCAN_BIN)

format:
	@echo ">> formatting code"
	@go fmt ./...

vet:
	@echo ">> vetting code"
	@go vet ./...

test:
	@echo ">> testing code"
	@go test ./... -count=1

linux: BIN_DIR=""
linux: clean format vet build

windows: BIN_DIR=""
windows: clean format vet build

build:
	@echo ">> building binaries"
	@CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/prometheus/common/version.Version=${APP_VERSION} \
			-X github.com/prometheus/common/version.Branch=${APP_BRANCH} \
			-X github.com/prometheus/common/version.Revision=${APP_REVISION} \
			-X github.com/prometheus/common/version.BuildUser=${APP_USER}@${APP_HOST} \
			-X github.com/prometheus/common/version.BuildDate=${APP_BUILD_DATE} \
		" \
		-o $(BIN_DIR)$(AMBOT_BIN) ./cmd/ambot
	@CGO_ENABLED=0 go build -v \
		-ldflags "-X github.com/prometheus/common/version.Version=${APP_VERSION} \
			-X github.com/prometheus/common/version.Branch=${APP_BRANCH} \
			-X github.com/prometheus/common/version.Revision=${APP_REVISION} \
			-X github.com/prometheus/common/version.BuildUser=${APP_USER}@${APP_HOST} \
			-X github.com/prometheus/common/version.BuildDate=${APP_BUILD_DATE} \
		" \
		-o $(BIN_DIR)$(ROUTE_SCAN_BIN) ./cmd/scanner
