CURRENT_DIR=$(shell pwd)
DIST_DIR=${CURRENT_DIR}/dist

.PHONY: release
release:
	make BIN_NAME=glooedge-plugin-darwin-amd64 GOOS=darwin glooedge-plugin-build
	make BIN_NAME=glooedge-plugin-darwin-arm64 GOOS=darwin GOARCH=arm64 glooedge-plugin-build
	make BIN_NAME=glooedge-plugin-linux-amd64 GOOS=linux glooedge-plugin-build
	make BIN_NAME=glooedge-plugin-linux-arm64 GOOS=linux GOARCH=arm64 glooedge-plugin-build
	make BIN_NAME=glooedge-plugin-windows-amd64.exe GOOS=windows glooedge-plugin-build

.PHONY: glooedge-plugin-build
glooedge-plugin-build:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCHP=${GOARCH} go build -v -o ${DIST_DIR}/${BIN_NAME} .

.PHONY: dev
dev:z
	kubectl create ns argo-rollouts || true
	skaffold dev -n argo-rollouts 

.PHONY: install-rollouts
install-rollouts:
	kubectl create ns argo-rollouts || true
	kubectl apply -k ./deploy

.PHONY: demo
demo:
	make install-rollouts
	kubectl apply -f ./examples/0-rollout-initial-state-green
