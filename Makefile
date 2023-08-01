DIR=$(strip $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST)))))

GOPATH      := $(GOPATH)
DATE         = $(shell date -u +%Y%m%d.%H%M%S.%Z)
GOGENERATE   = $(shell if [ -f .gogenerate ]; then cat .gogenerate; fi)
TESTPACKETS  = $(shell if [ -f .testpackages ]; then cat .testpackages; fi)
BENCHPACKETS = $(shell if [ -f .benchpackages ]; then cat .benchpackages; fi)

## Сценарий по умолчанию - отображение доступных команд.
default: help

## Загрузка и обновление зависимостей.
dep:
	@go clean -cache -modcache
	@go get -u ./...
	@go mod download
	@go mod tidy
.PHONY: dep

## Кодогенерация. Run only during development.
## All generating files are included in a .gogenerate file.
gen:
.PHONY: gen

## Запуск тестов.
test:
	@echo "mode: set" > $(DIR)/coverage.log
	@for PACKET in $(TESTPACKETS); do \
		touch $(DIR)/coverage-tmp.log; \
		GOPATH=${GOPATH} go test -v -covermode=count -coverprofile=$(DIR)/coverage-tmp.log $$PACKET; \
		if [ "$$?" -ne "0" ]; then exit $$?; fi; \
		tail -n +2 $(DIR)/coverage-tmp.log | sort -r | awk '{if($$1 != last) {print $$0;last=$$1}}' >> $(DIR)/coverage.log; \
		rm -f $(DIR)/coverage-tmp.log; true; \
	done
.PHONY: test

## Запуск тестов с отображением процента покрытия кода тестами.
cover: test
	@GOPATH=${GOPATH} go tool cover -html=$(DIR)/coverage.log
.PHONY: cover

## Запуск тестов производительности.
bench:
	@for PACKET in $(BENCHPACKETS); do GOPATH=${GOPATH} go test -race -bench=. -benchmem $$PACKET; done
.PHONY: bench

## Очистка от временных файлов.
clean:
	@rm -rf ${DIR}/src; true
	@rm -rf ${DIR}/bin; true
	@rm -rf ${DIR}/pkg; true
	@rm -rf ${DIR}/*.log; true
	@rm -rf ${DIR}/*.lock; true
.PHONY: clean

## Help for some targets.
help:
	@echo "Usage: make [target]"
	@echo "  target is:"
	@echo "    dep                  - Загрузка и обновление зависимостей."
	@echo "    gen                  - Кодогенерация."
	@echo "    test                 - Запуск тестов."
	@echo "    cover                - Запуск тестов с отображением процента покрытия кода тестами."
	@echo "    bench                - Запуск тестов производительности."
	@echo "    clean                - Очистка от временных файлов."
.PHONY: help
