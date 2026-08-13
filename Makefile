test:
	go test ./... -count=1

# Cross-compile the Windows tray build from any platform.
build:
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui" -o dist/wiretray.exe ./cmd/wiretray

# Regenerate icon assets and the exe resource block. Only needed when
# the mark, colors, or version metadata change.
icons:
	go run ./tray/gen
	go run github.com/tc-hib/go-winres@latest make --in winres/winres.json --out cmd/wiretray/rsrc --arch amd64

# Copy the build into place on the Windows side (WSL only). Quit the
# running app first; Windows locks executing files.
deploy: build
	cp dist/wiretray.exe "$$(wslpath "$$(cmd.exe /c 'echo %APPDATA%' 2>/dev/null | tr -d '\r')")/wiretray/wiretray.exe"

.PHONY: test build icons deploy
