
.PHONY: all test clean c-test go-test

all: test

test: c-test go-test

c-test:
	$(MAKE) -C lib/c test

go-test:
	$(MAKE) -C lib/go test

clean:
	$(MAKE) -C lib/c clean
	$(MAKE) -C lib/go clean