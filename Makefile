BIN_DIR := bin
CLI_OUT := $(BIN_DIR)/vectrigo-cli
LIB_OUT := $(BIN_DIR)/vectrigo.a

.PHONY: all lib cli clean capture-test-baseline

all: lib cli

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

lib: $(BIN_DIR)
	go build -buildmode=archive -o $(LIB_OUT) .

cli: $(BIN_DIR)
	go build -o $(CLI_OUT) ./cmd/vectrigo-cli

clean:
	rm -rf $(BIN_DIR)

# capture-test-baseline prints the per-architecture golden SHA256 digests used by
# the byte-identity tests (perf_test.go, autok_test.go). SVG coordinates are
# floating-point-derived and their last decimal can differ between architectures,
# so each digest is baselined per GOARCH. Copy the logged values into the `want`
# maps after an intentional output change.
capture-test-baseline:
	go test -run TestCaptureGoldenDigests -v .              # host arch (e.g. arm64)
	GOARCH=amd64 go test -run TestCaptureGoldenDigests -v . # cross-compile (Rosetta/qemu)
