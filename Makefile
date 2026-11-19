# CaddyMan follows the service-project convention: `build` makes one
# binary, `install` puts it in ~/.local/bin and the config template in
# ~/.config/caddyman/, `uninstall` removes the binary, `clean` the build.
# CaddyMan needs no secrets, so there is no .env file to install.

BIN        := caddyman
PREFIX     ?= $(HOME)/.local
BINDIR     := $(PREFIX)/bin
CONFIG_DIR := $(HOME)/.config/$(BIN)
CONFIG     := $(CONFIG_DIR)/config.json
TEMPLATE   := config.template.json

build:
	go build -o $(BIN) .

# An existing config is never changed without asking: Keep (default),
# Update (add the template's missing fields; nothing is removed or changed),
# or Replace (after a timestamped backup). Without a terminal it is kept.
install: build
	install -Dm755 $(BIN) $(BINDIR)/$(BIN)
	@echo "installed $(BINDIR)/$(BIN)"
	@set -e; \
	if [ ! -e "$(CONFIG)" ]; then \
		install -Dm644 $(TEMPLATE) "$(CONFIG)"; \
		echo "installed $(CONFIG) from $(TEMPLATE)"; \
	elif [ ! -t 0 ]; then \
		echo "kept the existing $(CONFIG) (no terminal to ask; run make install from a terminal to update or replace it)"; \
	else \
		printf '%s already exists. [K]eep it, [u]pdate it with new fields, or [r]eplace it (a backup is kept)? ' "$(CONFIG)"; \
		read answer; \
		case "$$answer" in \
		u|U) $(BINDIR)/$(BIN) config update "$(CONFIG)" ;; \
		r|R) backup="$(CONFIG).$$(date +%Y%m%d-%H%M%S).bak"; \
		     cp -p "$(CONFIG)" "$$backup"; \
		     install -m644 $(TEMPLATE) "$(CONFIG)"; \
		     echo "replaced $(CONFIG) (backup: $$backup)" ;; \
		*)   echo "kept $(CONFIG)" ;; \
		esac; \
	fi
	@echo "first time? see the README's Quick start, then run 'caddyman check'"

# Caddy, the site files CaddyMan wrote and the config are left alone.
uninstall:
	rm -f $(BINDIR)/$(BIN)
	@echo "removed $(BINDIR)/$(BIN); config kept in $(CONFIG_DIR); Caddy and the site files left as they are"

clean:
	rm -f $(BIN)

vet:
	go vet ./...

test:
	go test ./...

.PHONY: build install uninstall clean vet test
