SHELL := /bin/bash
PKGS := $(shell go list ./... | grep -v /vendor)
BIN_DIR := ./bin
GOLINT := $(GOPATH)/bin/golint
BUILD_NAME := orders


all: test build

test: lint
	@go test $(PKGS)

lint:
	@if [ ! -e $(GOLINT) ] ; then go get golang.org/x/lint/golint ; fi
	@exec $(GOLINT) .
	
build:
	@go build . 
	@mv $(BUILD_NAME) bin	
	@cp .env bin

run:
	@go run . 

dbup:
	@docker-compose -f postgre.yml up -d 

dbdown:
	@docker-compose -f postgre.yml down
	