BIN  := bin/matchtui
DIST := dist/matchtui

.PHONY: build test vet fmt run install clean release

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) .

# Builds every shippable target into dist/. Names follow uname, so a binary
# dropped onto a machine of that architecture just runs.
release:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/matchtui-linux-x86_64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/matchtui-linux-aarch64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/matchtui.exe .
	@ls -l dist

test:
	go test ./...

short:
	go test -short ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run: build
	$(BIN)

# Copies the binary for this machine into ~/.local/bin, the path every current
# distro puts on PATH. Use ./install.sh for the full flow with checks.
install: build
	@mkdir -p $(HOME)/.local/bin
	install -m 755 $(BIN) $(HOME)/.local/bin/matchtui
	@echo "installed $(HOME)/.local/bin/matchtui"

clean:
	rm -rf bin dist
