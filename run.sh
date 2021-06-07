#!/bin/bash

export PGHOST=localhost
export PGPORT=5455
export PGUSER=gorm
export PGDATABASE=gorm
export PGPASSWORD=gorm
export PGSSLMODE=disable
export SUFX="-G1"

#docker-compose -f docker-compose-dev.yml up -d --remove-orphans
./bin/orders
