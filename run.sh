#!/bin/bash

export PG_HOST=localhost
export PG_PORT=5432
export PG_USER=gorm
export PG_DBNAME=gorm
export PG_PWD=gorm
export PG_SSLMODE = disable
export SUFX="-G1"

./bin/orders
