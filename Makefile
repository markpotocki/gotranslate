.PHONY: build

build:
	sam build

test:
	cd ./translate && go test ./...
