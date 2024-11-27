
GO := go
COVERAGE_FILE := coverage.out

.PHONY: all test coverage bench clean

all: test

test:
	$(GO) test -v ./...

coverage:
	$(GO) test -v -coverprofile=$(COVERAGE_FILE) ./...
	$(GO) tool cover -html=$(COVERAGE_FILE)

bench:
	$(GO) test -bench=. -benchmem ./...

clean:
	rm -f $(COVERAGE_FILE)
	$(GO) clean