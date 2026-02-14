BINARY = pre-commit-checks
HOOK_TARGET = ../../.git/hooks/pre-commit

.PHONY: build install check clean

build:
	go build -o $(BINARY) .

install: build
	cp $(BINARY) $(HOOK_TARGET)
	chmod +x $(HOOK_TARGET)
	@echo "Installed pre-commit hook"

check: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
