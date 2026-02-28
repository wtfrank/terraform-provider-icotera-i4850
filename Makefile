HOSTNAME=local
NAMESPACE=icotera-i4850
NAME=icotera-i4850
BINARY=terraform-provider-${NAME}
VERSION=0.1.0

default: build

build:
	go build -o ${BINARY}

test: build
	# Forces terraform to use the new binary and show logs
	TF_LOG=DEBUG terraform plan
