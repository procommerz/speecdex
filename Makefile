GOOS ?= darwin
GOARCH ?= arm64
CGO_ENABLED ?= 0
BIN_NAME ?= speecdex
DIST_DIR ?= dist
GO_IMAGE ?= golang:1.22.12-bookworm
DOCKER_BUILDKIT ?= 1

.PHONY: docker-build docker-test clean

docker-build:
	mkdir -p $(DIST_DIR)
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build \
		--file Dockerfile.build \
		--target artifact \
		--build-arg GO_IMAGE=$(GO_IMAGE) \
		--build-arg GOOS=$(GOOS) \
		--build-arg GOARCH=$(GOARCH) \
		--build-arg CGO_ENABLED=$(CGO_ENABLED) \
		--build-arg BIN_NAME=$(BIN_NAME) \
		--output type=local,dest=$(DIST_DIR) \
		.

docker-test:
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build \
		--file Dockerfile.build \
		--target test \
		--build-arg GO_IMAGE=$(GO_IMAGE) \
		.

clean:
	rm -rf $(DIST_DIR)
