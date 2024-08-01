deploy:
	go build .
	docker build -t ourabridge .
	docker save ourabridge | ssh fakeflickr docker load
