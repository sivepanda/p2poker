.PHONY: sim build dispatch node clean

build:
	go build -o app ./cmd/sim
	go build -o dispatch ./cmd/dispatch
	go build -o node ./cmd/node

sim:
	go run ./cmd/sim

dispatch:
	go run ./cmd/dispatch

node:
	go run ./cmd/node

clean:
	rm -f app
	rm -f dispatch
	rm -f node
