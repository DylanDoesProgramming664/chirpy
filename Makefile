compile:
	go build -o chirpy

compile-run:
	go build -o chirpy
	./chirpy

run: main.go
	go run main.go
