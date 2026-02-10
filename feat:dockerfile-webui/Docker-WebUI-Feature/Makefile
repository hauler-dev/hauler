.PHONY: build run stop clean logs

build:
	docker-compose build

run:
	docker-compose up -d

stop:
	docker-compose down

clean:
	docker-compose down -v
	rm -rf data/store/* data/hauls/* data/config/*

logs:
	docker-compose logs -f

restart:
	docker-compose restart

shell:
	docker exec -it hauler-ui sh
