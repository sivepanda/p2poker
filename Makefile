.PHONY: sim build clean

build:
	go build -o app ./cmd/sim

sim:
	go run ./cmd/sim

clean:
	rm -f app
