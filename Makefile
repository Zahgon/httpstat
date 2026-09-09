.PHONY: test unit e2e build clean

build:
	go build -o httpstat .

unit:
	go test -v ./...

e2e: build
	@bash httpstat_test.sh

test: unit

clean:
	rm -f httpstat
