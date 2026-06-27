BIN_DIR := ./bin
BIN_DST := /usr/bin
FRP_DIR := ./frp
FRP_VERSION := 0.69.1

ifdef GOOS
	ifeq (${GOOS}, windows)
		WIN_TARGET := True
	endif
else
	ifeq (${OS}, Windows_NT)
		WIN_TARGET := True
	endif
endif

ifdef WIN_TARGET
	TSSH := tssh.exe
else
	TSSH := tssh
endif

ifeq (${OS}, Windows_NT)
	RM := PowerShell -Command Remove-Item -Force
	GO_TEST := go test
else
	RM := rm -f
	GO_TEST := ${shell basename `which gotest 2>/dev/null` 2>/dev/null || echo go test}
endif

.PHONY: all clean test install download-frp

# Check if frp source is available (may not exist on clone since /frp/ is gitignored)
ifneq ("$(wildcard $(FRP_DIR)/go.mod)","")
  HAS_FRP := yes
endif

all: ${BIN_DIR}/${TSSH}

ifeq ($(HAS_FRP),yes)
all: ${BIN_DIR}/frps ${BIN_DIR}/frpc
endif

${BIN_DIR}/${TSSH}: $(wildcard ./cmd/tssh/*.go ./tssh/*.go) go.mod go.sum
	CGO_ENABLED=0 go build -o ${BIN_DIR}/ ./cmd/tssh

ifeq ($(HAS_FRP),yes)
${BIN_DIR}/frps: ${FRP_DIR}/go.mod ${FRP_DIR}/go.sum
	cd ${FRP_DIR} && CGO_ENABLED=0 go build -tags noweb -o ../${BIN_DIR}/frps -ldflags="-s -w" ./cmd/frps

${BIN_DIR}/frpc: ${FRP_DIR}/go.mod ${FRP_DIR}/go.sum
	cd ${FRP_DIR} && CGO_ENABLED=0 go build -tags noweb -o ../${BIN_DIR}/frpc -ldflags="-s -w" ./cmd/frpc
endif

# Download pre-built FRP binaries (used when /frp/ source is not available)
FRP_OS := $(shell uname -s | tr A-Z a-z)
FRP_ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/armv7l/arm_hf/;s/armv6l/arm/;s/i386/386/')
download-frp:
	@mkdir -p ${BIN_DIR}
	@if [ "${FRP_OS}" = "darwin" ] && [ "${FRP_ARCH}" = "arm64" ]; then \
		echo "Downloading FRP v${FRP_VERSION} for darwin_arm64..."; \
		curl -sLo /tmp/frp.tar.gz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_darwin_arm64.tar.gz"; \
	elif [ "${FRP_OS}" = "darwin" ]; then \
		echo "Downloading FRP v${FRP_VERSION} for darwin_amd64..."; \
		curl -sLo /tmp/frp.tar.gz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_darwin_amd64.tar.gz"; \
	else \
		echo "Downloading FRP v${FRP_VERSION} for ${FRP_OS}_${FRP_ARCH}..."; \
		curl -sLo /tmp/frp.tar.gz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_${FRP_OS}_${FRP_ARCH}.tar.gz" || \
		( echo "Trying arm fallback..."; \
		  ARCH_ARM=$$(echo "${FRP_ARCH}" | sed 's/arm_hf/arm/'); \
		  curl -sLo /tmp/frp.tar.gz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_${FRP_OS}_$${ARCH_ARM}.tar.gz" ); \
	fi && \
	tar -xzf /tmp/frp.tar.gz -C /tmp && \
	cp /tmp/frp_${FRP_VERSION}_*/frps ${BIN_DIR}/ && \
	cp /tmp/frp_${FRP_VERSION}_*/frpc ${BIN_DIR}/ && \
	chmod +x ${BIN_DIR}/frps ${BIN_DIR}/frpc && \
	rm -f /tmp/frp.tar.gz && \
	echo "FRP binaries installed in ${BIN_DIR}/"

clean:
	$(RM) ${BIN_DIR}/tssh ${BIN_DIR}/tssh.exe ${BIN_DIR}/frps ${BIN_DIR}/frpc 2>/dev/null; true

test:
	${GO_TEST} -v -count=1 ./tssh

install: all
ifdef WIN_TARGET
	@echo install target is not supported for Windows
else
	@mkdir -p ${DESTDIR}${BIN_DST}
	cp ${BIN_DIR}/tssh ${DESTDIR}${BIN_DST}/
endif
