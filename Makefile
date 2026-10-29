# Top-level build helpers.
#   make apk      build the TV app (release) and stage it for embedding in the server
#   make server   build the server binary with the staged APK embedded
#   make docker   build the Docker image (expects `make apk` first, or an APK in server/self/)
#   make run      run the server locally against ./data
#   make snapshot untagged goreleaser build: dist/ gets tarballs + .deb for amd64/arm64
#   make release  tagged goreleaser build, published as a GitHub release (needs GITHUB_TOKEN)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
APK = tv/app/build/outputs/apk/release/app-release.apk

.PHONY: apk server docker snapshot release check run assets clean

# APK version follows git (tools/version.sh): versionName from `git describe`,
# versionCode from the nearest v* tag.
apk:
	cd tv && ATVLOADER_VERSION_NAME=$$(../tools/version.sh | sed -n 1p) ATVLOADER_VERSION_CODE=$$(../tools/version.sh | sed -n 2p) ./gradlew assembleRelease
	cp $(APK) server/self/atvloader.apk

server:
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o ../dist/atvloader-server .

docker:
	docker compose build --build-arg VERSION=$(VERSION)

# goreleaser drives .goreleaser.yaml: builds server/ for amd64+arm64 and
# packages the .deb with the systemd unit. Both expect the APK staged by
# `make apk` (the before-hook refuses to build without it).
snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

check:
	goreleaser check

run: server
	./dist/atvloader-server -data ./data

assets:
	cd tools/genassets && go run . ../../tv/app/src/main/res

clean:
	rm -rf dist server/self/*.apk
	cd tv && ./gradlew clean
