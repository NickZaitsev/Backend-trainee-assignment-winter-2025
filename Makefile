.PHONY: up down build logs test clean

up:
	docker-compose up --build

down:
	docker-compose down

build:
	docker-compose build

logs:
	docker-compose logs -f

test:
	go test -v ./...

clean:
	docker-compose down -v
	rm -rf *.log

help:
	@echo "Available commands:"
	@echo "  make up     - Start the service with docker-compose"
	@echo "  make down   - Stop the service"
	@echo "  make build  - Build the docker images"
	@echo "  make logs   - View logs"
	@echo "  make test   - Run tests"
	@echo "  make clean  - Clean up containers and volumes"
