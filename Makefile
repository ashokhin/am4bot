GOVERSION	:= $(shell go env GOVERSION)
GOARCH		:= $(shell go env GOARCH)
GOOS		:= $(shell go env GOOS)

BIN_DIR		?= $(shell pwd)/bin/
EXEC_EXT	?= $(shell go env GOEXE)

AMBOT_BIN		:= ambot$(EXEC_EXT)
ROUTE_SCAN_BIN	:= scanner$(EXEC_EXT)

export APP_BRANCH		?= $(shell git describe --all --contains --dirty HEAD)
export APP_VERSION		?= $(shell basename ${APP_BRANCH})
export APP_REVISION		?= $(shell git rev-parse HEAD)
export APP_USER			?= $(shell id -u --name)
export APP_HOST			?= $(shell hostname)
export APP_BUILD_DATE	?= $(shell date -u '+%Y-%m-%dT%H:%M:%S,%N%:z')

RELEASE_DIR		:= ambot-release

#$(error GOOS env variable is missing "$(GOOS)")

ifeq ($(GOOS),windows)
	ARCHIVE_CMD ?= zip -vjr ../${RELEASE_NAME}.zip *
else ifeq ($(GOOS),linux)
	ARCHIVE_CMD ?= tar -cvzf ../${RELEASE_NAME}.tar.gz *
else ifeq ($(GOOS),)
	ARCHIVE_CMD := ""
$(error GOOS env variable is missing)
else
	ARCHIVE_CMD := ""
$(error OS is unsupported)
endif

.PHONY: all
all: clean format vet test build

clean:
	@echo ">> removing build artifacts"
	@rm -rf $(BIN_DIR)

format:
	@echo ">> formatting code"
	@go fmt ./...

vet:
	@echo ">> vetting code"
	@go vet ./...

test:
	@echo ">> testing code"
	@go test ./... -count=1

build:
	@echo ">> building binaries"
	@mkdir -p $(BIN_DIR)
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


release: all
	@echo ">> creating release"
	@if [ -z "${RELEASE_NAME}" ]; then \
		echo "The 'RELEASE_NAME' env variable is missing but required for creating release"; \
		exit 1; \
	fi
	@rm -rf ./$(RELEASE_DIR)
	@mkdir -p ./$(RELEASE_DIR)
	@cp $(BIN_DIR)$(AMBOT_BIN) $(BIN_DIR)$(ROUTE_SCAN_BIN) LICENSE README.md ./$(RELEASE_DIR)
	@cd ./$(RELEASE_DIR) ; $(ARCHIVE_CMD)
	@cd ./$(RELEASE_DIR) ; ls -alh
	@rm -rf ./$(RELEASE_DIR)
