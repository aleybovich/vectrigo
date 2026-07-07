BIN_DIR := bin
CLI_OUT := $(BIN_DIR)/vectrigo-cli
LIB_OUT := $(BIN_DIR)/vectrigo.a

.PHONY: all lib cli clean

all: lib cli

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

lib: $(BIN_DIR)
	go build -buildmode=archive -o $(LIB_OUT) .

cli: $(BIN_DIR)
	go build -o $(CLI_OUT) ./cmd/vectrigo-cli

clean:
	rm -rf $(BIN_DIR)
