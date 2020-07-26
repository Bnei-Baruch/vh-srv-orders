SHELL := /bin/bash
PKGS := $(shell go list ./... | grep -v /vendor)
BIN_DIR := ./bin
GOLINT := $(GOPATH)/bin/golint
BUILD_NAME := orders
INSTAL_DIR := /usr/local/bin/


all: test build

test: lint
	@go test $(PKGS)

lint:
	@if [ ! -e $(GOLINT) ] ; then go get golang.org/x/lint/golint ; fi
	@exec $(GOLINT) .
	
build:
	@go build . 
	@mkdir -p bin
	@mv $(BUILD_NAME) bin	
	@cp .env bin

run:
	@go run . 

dbup:
	@docker-compose -f postgre.yml up -d 

dbdown:
	@docker-compose -f postgre.yml down

dbimport:
	@docker stop bborders_go_dev
	@docker rm bborders_go_dev
	@docker-compose -f postgre.yml up -d 
	@sleep 10
	@docker exec -i bborders_go_dev psql -U gorm gorm < ./data/import.pgsql


install: 
	@cp  $(BIN_DIR)/$(BUILD_NAME) $(INSTAL_DIR)

config:
	@echo "todo : add service config here"

update:
	@sudo systemctl stop orders.service
	@cp  $(BIN_DIR)/$(BUILD_NAME) $(INSTAL_DIR)
	@sudo systemctl start orders.service

