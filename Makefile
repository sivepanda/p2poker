.PHONY: sim build dispatch node clean proto

build:
	go build -o app ./cmd/sim
	go build -o dispatch ./cmd/dispatch
	go build -o node ./cmd/node
	go build -o sim ./cmd/sim

sim:
	go build -o sim ./cmd/sim

dispatch:
	go build -o dispatch ./cmd/dispatch

node:
	go build -o node ./cmd/node

proto:
	protoc --go_out=. --go_opt=module=github.com/sivepanda/p2poker \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/sivepanda/p2poker \
	       proto/clientrpc/v1/clientrpc.proto

clean:
	rm -f app
	rm -f dispatch
	rm -f node
	rm -f sim
