build:
	go build -o bin/ocgo2cli .

test:
	go test -race ./...

lint:
	go vet ./...

clean:
	rm -rf bin/
