.PHONY: test lint

test:
	docker-compose up -d db-migration
	sleep 1
	go test -count=1 -p=1 ./...

lint:
	go mod tidy
	golangci-lint run --color always -v -c ./.golangci.yml

poc:
	docker-compose up -d db-migration
	sleep 3
	docker-compose up -d poc-web-service poc-polling-worker device-simulator-1 device-simulator-2 device-simulator-3	
