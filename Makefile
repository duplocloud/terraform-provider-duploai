TEST      ?=$$(go list ./duplocloud/... ./duplosdk/...)
HOSTNAME  = registry.terraform.io
NAMESPACE = duplocloud
NAME      = duplocloud-helpdesk
BINARY    = terraform-provider-${NAME}
VERSION   = 0.0.1
OS_ARCH  := $$(go env GOOS)_$$(go env GOARCH)
OS_ARCH_MAC    = darwin_amd64
OS_ARCH_DOCKER = linux_amd64
duplo_host  ?= http://localhost:60021
duplo_token ?= FAKE

default: install

doc:
	go generate

build:
	go build -o ${BINARY}

release:
	GOOS=darwin  GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_darwin_amd64
	GOOS=linux   GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_linux_amd64
	GOOS=linux   GOARCH=arm   go build -o ./bin/${BINARY}_${VERSION}_linux_arm
	GOOS=windows GOARCH=386   go build -o ./bin/${BINARY}_${VERSION}_windows_386
	GOOS=windows GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_windows_amd64

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mkdir -p ~/.terraform.d/plugin-cache/${OS_ARCH}
	cp ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}/${BINARY} \
	   ~/.terraform.d/plugin-cache/${OS_ARCH}/terraform-provider-${NAME}_v${VERSION}_x4

install_mac: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH_MAC}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH_MAC}

install_docker: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH_DOCKER}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH_DOCKER}

test:
	go test -i $$(TEST) || exit 1
	echo $$(TEST) | xargs -t -n4 go test $$(TESTARGS) -timeout=90s -parallel=4

testacc:
	TF_ACC=1 go test $$(TEST) -v $$(TESTARGS) -timeout 120m

vet:
	go vet ./...

lint:
	golangci-lint run ./...
