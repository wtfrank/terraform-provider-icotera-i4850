HOSTNAME=local
NAMESPACE=icotera-i4850
NAME=icotera-i4850
BINARY=terraform-provider-${NAME}
VERSION=0.1.0

default: build

build:
	go fmt ./... && \
          go build -o ${BINARY}

test: build
	# Forces terraform to use the new binary and show logs
	TF_LOG=DEBUG terraform plan

.PHONY: docs
docs:
	go run -modfile tools/go.mod github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --rendered-provider-name "Icotera i4850"  --provider-dir .

.PHONY: lint
lint:
	go run -modfile tools/go.mod github.com/golangci/golangci-lint/cmd/golangci-lint run ./...
