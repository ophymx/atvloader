# Top-level build helpers.
#   make apk      build the TV app (release) and stage it for embedding in the server
#   make server   build the server binary with the staged APK embedded
#   make docker   build the Docker image (expects `make apk` first, or an APK in server/self/)
#   make run      run the server locally against ./data

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
APK = tv/app/build/outputs/apk/release/app-release.apk

.PHONY: apk server docker run assets clean

apk:
	cd tv && ./gradlew assembleRelease
	cp $(APK) server/self/atvloader.apk

server:
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o ../dist/atvloader-server .

docker:
	docker compose build --build-arg VERSION=$(VERSION)

run: server
	./dist/atvloader-server -data ./data

assets:
	cd tools/genassets && go run . ../../tv/app/src/main/res

clean:
	rm -rf dist server/self/*.apk
	cd tv && ./gradlew clean
