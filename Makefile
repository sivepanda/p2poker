.PHONY: run build clean

run:
	cd prototype && go run .

build:
	cd prototype && go build -o ../app .

clean:
	rm -f app