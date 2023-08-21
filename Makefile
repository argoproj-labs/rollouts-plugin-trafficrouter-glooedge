CURRENT_DIR=$(shell pwd)
DIST_DIR=${CURRENT_DIR}/dist

.PHONY: release
release:
	rm ${DIST_DIR}/checksums.txt || true
	make BIN_NAME=glooedge-plugin-darwin-amd64 GOOS=darwin glooedge-plugin-build
	sha256sum dist/glooedge-plugin-darwin-amd64 >> dist/checksums.txt
	make BIN_NAME=glooedge-plugin-darwin-arm64 GOOS=darwin GOARCH=arm64 glooedge-plugin-build
	sha256sum dist/glooedge-plugin-darwin-arm64 >> dist/checksums.txt
	make BIN_NAME=glooedge-plugin-linux-amd64 GOOS=linux glooedge-plugin-build
	sha256sum dist/glooedge-plugin-linux-amd64 >> dist/checksums.txt
	make BIN_NAME=glooedge-plugin-linux-arm64 GOOS=linux GOARCH=arm64 glooedge-plugin-build
	sha256sum dist/glooedge-plugin-linux-arm64 >> dist/checksums.txt
	make BIN_NAME=glooedge-plugin-windows-amd64.exe GOOS=windows glooedge-plugin-build
	sha256sum dist/glooedge-plugin-windows-amd64.exe >> dist/checksums.txt

.PHONY: glooedge-plugin-build
glooedge-plugin-build:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -v -o ${DIST_DIR}/${BIN_NAME} .

.PHONY: install-rollouts
install-rollouts:
	kubectl create ns argo-rollouts || true
	kubectl apply -k ./deploy
