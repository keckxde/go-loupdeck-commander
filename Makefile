# CMake + CMakePresets + CPack Offline Go Build Makefile (Git-Tag Versioned)
CMAKE       ?= cmake
CTEST       ?= ctest
CPACK       ?= cpack
GO          ?= go
PRESET      ?= default
BINARY_NAME ?= loupedeck-commander
CMD_PKG     := ./cmd/loupedeck-commander

# Enforce 100% offline build from ./vendor
export GOPROXY  := off
export GOSUMDB  := off
export GOFLAGS  := -mod=vendor

# --- Git-Tag Based Versioning Scheme ---
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DESC   := $(shell git describe --tags --long --dirty --match "v*" 2>/dev/null || git describe --tags --long --dirty 2>/dev/null || echo "")
ifeq ($(GIT_DESC),)
  ifneq ($(GIT_COMMIT),unknown)
    GIT_DIRTY   := $(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo "-dirty" || echo "")
    GIT_VERSION := v0.1.0-0-g$(GIT_COMMIT)$(GIT_DIRTY)
  else
    GIT_VERSION := v0.1.0-dev
  endif
else
  # Collapse exact tag "<tag>-0-g<sha>" -> "<tag>" and "<tag>-0-g<sha>-dirty" -> "<tag>-dirty"
  GIT_VERSION := $(shell echo "$(GIT_DESC)" | sed -E 's/^([^v])/v\1/; s/-0-g[0-9a-fA-F]+$$//; s/-0-g[0-9a-fA-F]+(-dirty)$$/\1/')
endif
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -s -w -X main.Version=$(GIT_VERSION) -X main.Commit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

.PHONY: all configure build build-linux build-windows test test-race cpack package package-deb package-tgz package-windows workflow version vendor clean

all: build

version:
	@echo "Version:    $(GIT_VERSION)"
	@echo "Commit:     $(GIT_COMMIT)"
	@echo "BuildTime:  $(BUILD_TIME)"

configure:
	$(CMAKE) --preset $(PRESET)

build: configure
	$(CMAKE) --build --preset $(PRESET)

build-linux:
	$(CMAKE) --preset linux
	$(CMAKE) --build --preset linux

build-windows:
	$(CMAKE) --preset windows
	$(CMAKE) --build --preset windows

test: build
	$(CTEST) --preset $(PRESET)

test-race:
	$(CMAKE) --preset race
	$(CTEST) --preset race

cpack: package

package: build
	$(CPACK) --preset $(PRESET)

package-deb:
	$(CMAKE) --preset release
	$(CMAKE) --build --preset release
	$(CPACK) --preset deb

package-tgz:
	$(CMAKE) --preset release
	$(CMAKE) --build --preset release
	$(CPACK) --preset tgz

package-windows: build-windows
	$(CPACK) --preset windows

workflow:
	$(CMAKE) --workflow --preset $(PRESET)

# Run only when online to refresh ./vendor from go.mod
vendor:
	GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org $(GO) mod tidy
	GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org $(GO) mod vendor
	$(GO) mod verify

clean:
	rm -rf build build-RELEASE build-DEBUG build-RACE build-WINDOWS build-NINJA
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe loupedeck-emulator loupedeck-emulator.cmd
