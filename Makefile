BIN_DIR := ./bin
BIN_DST := /usr/bin
FRP_DIR := ./frp

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

.PHONY: all clean test install

all: ${BIN_DIR}/${TSSH} ${BIN_DIR}/frps ${BIN_DIR}/frpc

${BIN_DIR}/${TSSH}: $(wildcard ./cmd/tssh/*.go ./tssh/*.go) go.mod go.sum
	CGO_ENABLED=0 go build -o ${BIN_DIR}/ ./cmd/tssh

${BIN_DIR}/frps: ${FRP_DIR}/go.mod ${FRP_DIR}/go.sum
	cd ${FRP_DIR} && CGO_ENABLED=0 go build -tags noweb -o ../${BIN_DIR}/frps -ldflags="-s -w" ./cmd/frps

${BIN_DIR}/frpc: ${FRP_DIR}/go.mod ${FRP_DIR}/go.sum
	cd ${FRP_DIR} && CGO_ENABLED=0 go build -tags noweb -o ../${BIN_DIR}/frpc -ldflags="-s -w" ./cmd/frpc

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
