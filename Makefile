# Makefile для сборки бинарника metrixd под разные архитектуры
# и копирования его в каталог files роли Ansible. Предполагается,
# что локально установлен Go. Подробности — в README.md.

APP := metrixd
PKG := .
# Извлекаем текущий тег Git или хеш коммита для встраивания в бинарник.
# Если тегов нет — используем "dev". Версия передаётся компоновщику Go
# через -ldflags.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X 'main.version=$(VERSION)'

.PHONY: build-linux-arm64 build-linux-amd64 clean

# Собрать статический Linux-бинарник для aarch64/ARM64.
# Эту архитектуру использует Rocky Linux 9.6 на Apple Silicon (UTM).
build-linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o roles/metrixd/files/$(APP) $(PKG)
	@chmod +x roles/metrixd/files/$(APP)
	@echo "Собрано: roles/metrixd/files/$(APP) для linux/arm64"

# Собрать статический Linux-бинарник для amd64 (x86_64).
# В этом репозитории не обязателен, добавлен для полноты.
build-linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o roles/metrixd/files/$(APP) $(PKG)
	@chmod +x roles/metrixd/files/$(APP)
	@echo "Собрано: roles/metrixd/files/$(APP) для linux/amd64"

# Очистить собранный бинарник в роли.
clean:
	rm -f roles/metrixd/files/$(APP)
	@echo "Удалено: roles/metrixd/files/$(APP)"
