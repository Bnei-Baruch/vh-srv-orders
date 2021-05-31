#!/bin/bash

export PGHOST=localhost
export PGPORT=5435
export PGUSER=gorm
export PGDATABASE=gorm
export PGPASSWORD=gorm
export PGSSLMODE=disable
export SUFX="-G1"

./bin/orders
