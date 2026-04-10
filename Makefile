.PHONY: sim build dispatch node clean proto

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

proto:
	protoc --go_out=. --go_opt=module=github.com/sivepanda/p2poker \
	       --go-grpc_out=. --go-grpc_opt=module=github.com/sivepanda/p2poker \
	       proto/clientrpc/v1/clientrpc.proto

clean:
	rm -f app
	rm -f dispatch
	rm -f node
