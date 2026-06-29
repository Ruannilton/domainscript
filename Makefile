# Makefile do front-end do transpilador DomainScript.
# Alvos: build, test, lint, fmt. Requer Go no PATH; lint usa gofmt + go vet.

.PHONY: all build test lint fmt vet fmt-check

all: build

## build: compila todos os pacotes
build:
	go build ./...

## test: roda toda a suíte de testes
test:
	go test ./...

## lint: checa formatação (gofmt) e executa go vet
lint: fmt-check vet

## vet: análise estática do go vet
vet:
	go vet ./...

## fmt-check: falha se algum arquivo não estiver formatado
fmt-check:
	@gofmt -l . | tee /dev/stderr | (! read)

## fmt: formata todos os arquivos Go in-place
fmt:
	gofmt -w .
