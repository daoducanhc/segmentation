# Root Makefile - delegates to backend/Makefile

.PHONY: init api swagger build run migrate migrate-soft clean all help

init:
	cd backend && $(MAKE) init

api:
	cd backend && $(MAKE) api

swagger:
	cd backend && $(MAKE) swagger

build:
	cd backend && $(MAKE) build

run:
	cd backend && $(MAKE) run

migrate:
	cd backend && $(MAKE) migrate

migrate-soft:
	cd backend && $(MAKE) migrate-soft

clean:
	cd backend && $(MAKE) clean

all:
	cd backend && $(MAKE) all

help:
	cd backend && $(MAKE) help
